package device

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/TechBlueprints/disunified/internal/devicemodel"
)

func TestReportedAggregationsEstablishCapacityWithoutWriteCapability(t *testing.T) {
	desc := testDesc()
	desc.UDAPIVersion = "1.0.0"
	s := NewSession(desc, "http://192.0.2.1:8080/inform", State{Adopted: true}, nil, time.Now())
	s.SetCapabilities(devicemodel.Capabilities{LAGReporting: true})
	s.SetSnapshot(aggregationSnapshot())
	got := payload(t, s)["switch_caps"].(map[string]any)
	if got["max_aggregate_sessions"] != float64(2) {
		t.Fatalf("reported capacity=%v", got)
	}
	if int(got["feature_caps"].(float64))&swCapLACP != 0 {
		t.Fatal("reporting enabled LACP configuration")
	}
}

func aggregationSnapshot() *devicemodel.Snapshot {
	return &devicemodel.Snapshot{
		System: devicemodel.System{MAC: "02:00:00:00:00:01", Serial: "SSJ00000000"},
		Ports: []devicemodel.Port{
			{Index: 2, IfName: "port2", Present: true, Enabled: true, Up: true, SpeedMbps: 1000, LAG: "bundle7", LAGID: 7},
			{Index: 4, IfName: "port4", Present: true, Enabled: true, Up: true, SpeedMbps: 1000, LAG: "bundle7", LAGID: 7},
			{Index: 6, IfName: "port6", Present: true, Enabled: true, Up: true, SpeedMbps: 10000, LAG: "bundle11", LAGID: 11},
			{Index: 9, IfName: "port9", Present: true, Enabled: true, Up: true, SpeedMbps: 10000, LAG: "bundle11", LAGID: 11},
		},
		Aggregations: []devicemodel.Aggregation{
			{ID: 7, Name: "bundle7", Up: true, HasCounters: true, Counters: devicemodel.Counters{RxBytes: 1200, TxBytes: 3400}, Members: []devicemodel.AggregationMember{{PortIndex: 2, Active: true, SpeedMbps: 1000}, {PortIndex: 4, Active: true, SpeedMbps: 1000}}},
			{ID: 11, Name: "bundle11", Up: true, HasCounters: true, Counters: devicemodel.Counters{RxBytes: 5600, TxBytes: 7800}, Members: []devicemodel.AggregationMember{{PortIndex: 6, Active: true, SpeedMbps: 10000}, {PortIndex: 9, Active: true, SpeedMbps: 10000}}},
		},
	}
}

func TestAggregatePayloadMatchesCapturedMemberStructure(t *testing.T) {
	s := aggregationSnapshot()
	desc, err := DescriptorFor("UDC48X6", s, Identity{MAC: s.System.MAC, Serial: s.System.Serial, IP: "192.0.2.9"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(portTable(desc, s, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(b, &rows); err != nil {
		t.Fatal(err)
	}
	var reference map[string]any
	for _, row := range loadReference(t, "inform-ecs-aggregation-uswf066.json")["port_table"].([]any) {
		r := row.(map[string]any)
		if r["aggregate_num_ports"] != nil {
			reference = r
			break
		}
	}
	if reference == nil {
		t.Fatal("reference lacks an aggregate")
	}
	for _, group := range s.Aggregations {
		master := group.Members[0].PortIndex
		row := rows[master-1]
		if row["rx_bytes"] != float64(group.Counters.RxBytes) || row["tx_bytes"] != float64(group.Counters.TxBytes) {
			t.Errorf("aggregate counters at port %d are rx=%v tx=%v, want %d/%d", master, row["rx_bytes"], row["tx_bytes"], group.Counters.RxBytes, group.Counters.TxBytes)
		}
		if row["aggregate_num_ports"] != float64(len(group.Members)) || row["lag_member"] != true {
			t.Errorf("aggregate row %d missing group fields", master)
		}
		states, ok := row["lacp_state"].([]any)
		if !ok || len(states) != len(group.Members) {
			t.Fatalf("member states=%v", row["lacp_state"])
		}
		real := reference["lacp_state"].([]any)[0].(map[string]any)
		for _, entry := range states {
			state := entry.(map[string]any)
			for key, value := range real {
				if jsonType(state[key]) != jsonType(value) {
					t.Errorf("member key %s has type %T, want %s", key, state[key], jsonType(value))
				}
			}
		}
		if reflect.TypeOf(row["lag_member"]) != reflect.TypeOf(reference["lag_member"]) {
			t.Error("lag_member type differs")
		}
		for _, m := range group.Members {
			memberRow := rows[m.PortIndex-1]
			if memberRow["lag_member"] != true {
				t.Errorf("port %d not marked member", m.PortIndex)
			}
			if memberRow["lag_idx"] != float64(group.ID) {
				t.Errorf("port %d group ID=%v, want %d", m.PortIndex, memberRow["lag_idx"], group.ID)
			}
			if m.PortIndex == master {
				if memberRow["masked"] != false || memberRow["aggregated_by"] != false || memberRow["op_mode"] != "aggregate" {
					t.Errorf("primary relationship=%v", memberRow)
				}
				if len(memberRow["aggregate_members"].([]any)) != len(group.Members) {
					t.Error("primary lacks member list")
				}
			} else {
				if memberRow["masked"] != true || memberRow["aggregated_by"] != float64(master) || memberRow["op_mode"] != "switch" {
					t.Errorf("secondary relationship=%v", memberRow)
				}
			}
		}
	}
}

func TestAggregateUplinkUsesLogicalPrimary(t *testing.T) {
	s := aggregationSnapshot()
	group := s.Aggregations[0]
	primary, secondary := group.Members[0].PortIndex, group.Members[1].PortIndex
	desc, err := DescriptorFor("UDC48X6", s, Identity{MAC: s.System.MAC, Serial: s.System.Serial, IP: "192.0.2.9", UplinkPort: secondary})
	if err != nil {
		t.Fatal(err)
	}
	rows := portTable(desc, s, nil, nil)
	if rows[primary-1]["is_uplink"] != true || rows[secondary-1]["is_uplink"] != false {
		t.Fatal("uplink flag did not move to logical primary")
	}
	groups := deviceTables(desc, s)["link_aggregation_groups"].([]map[string]any)
	if len(groups) != len(s.Aggregations) || groups[0]["lag_idx"] != group.ID {
		t.Errorf("group table=%v", groups)
	}
}
