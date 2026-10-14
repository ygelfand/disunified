package unifiapi

import (
	"testing"

	"github.com/TechBlueprints/disunified/internal/devicemodel"
)

func TestSeedAggregations(t *testing.T) {
	snapshot := &devicemodel.Snapshot{
		Ports: []devicemodel.Port{
			{Index: 3, Present: true, VLAN: devicemodel.PortVLAN{Mode: "trunk", NativeVLAN: 20, AllowAll: true}},
			{Index: 8, Present: true, Enabled: true, VLAN: devicemodel.PortVLAN{Mode: "trunk", NativeVLAN: 20, AllowAll: true}},
		},
		Aggregations: []devicemodel.Aggregation{{ID: 7, Name: "bundle7", Members: []devicemodel.AggregationMember{{PortIndex: 8}, {PortIndex: 3}}}},
	}
	nets := []Network{{ID: "native", VLAN: 20, Enabled: true}}
	for _, tc := range []struct {
		name     string
		existing map[int]map[string]any
		want     int
	}{
		{name: "new group with down primary", existing: map[int]map[string]any{3: {"port_idx": 3, "name": "Operator name"}}, want: 1},
		{name: "configured profile", existing: map[int]map[string]any{8: {"port_idx": 8, "portconf_id": "profile"}}},
		{name: "overlapping group", existing: map[int]map[string]any{2: {"port_idx": 2, "op_mode": "aggregate", "aggregate_members": []any{float64(2), float64(8)}}}},
		{name: "no existing overrides", existing: map[int]map[string]any{}, want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			count, _ := seedAggregations(snapshot, nets, tc.existing)
			if count != tc.want {
				t.Fatalf("seeded %d, want %d", count, tc.want)
			}
			if count == 0 {
				return
			}
			o := tc.existing[3]
			if !sameJSON(o["aggregate_members"], []int{3, 8}) || o["lag_idx"] != nil || o["native_networkconf_id"] != "native" || o["forward"] == "disabled" {
				t.Fatalf("unexpected override: %v", o)
			}
			if count, _ := seedAggregations(snapshot, nets, tc.existing); count != 0 {
				t.Fatal("second reconciliation was not idempotent")
			}
		})
	}
	if count, notes := seedAggregations(snapshot, nil, map[int]map[string]any{}); count != 0 || len(notes) != 1 {
		t.Fatal("unknown VLAN was not skipped")
	}
	snapshot.Aggregations = nil
	for i := range snapshot.Ports {
		snapshot.Ports[i].LAGID = 7
		snapshot.Ports[i].LAG = "bundle7"
	}
	if count, _ := seedAggregations(snapshot, nets, map[int]map[string]any{}); count != 1 {
		t.Fatal("port membership from other drivers was not seeded")
	}
}
