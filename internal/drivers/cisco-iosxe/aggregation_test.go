package ciscoiosxe

import (
	"testing"

	"github.com/TechBlueprints/disunified/internal/devicemodel"
)

func TestOperationalAggregateMembers(t *testing.T) {
	_, _, s := fixtureCollector(t)
	if len(s.Aggregations) == 0 {
		t.Fatal("operational aggregate capture produced no groups")
	}
	for _, g := range s.Aggregations {
		if len(g.Members) == 0 || g.ID <= 0 {
			t.Errorf("group=%+v", g)
		}
		for _, m := range g.Members {
			p := s.Ports[m.PortIndex-1]
			if m.SpeedMbps != p.SpeedMbps || p.LAG != g.Name || (m.Active && (!g.Up || !p.Up)) {
				t.Errorf("member=%+v in %s", m, g.Name)
			}
		}
	}
}

func TestAggregateCountersComeFromPortChannelInterface(t *testing.T) {
	snapshot := &devicemodel.Snapshot{Ports: []devicemodel.Port{
		{Index: 7, IfName: "GigabitEthernet2/0/7", Present: true, LAG: "Port-channel42", LAGID: 42},
		{Index: 8, IfName: "GigabitEthernet2/0/8", Present: true, LAG: "Port-channel42", LAGID: 42},
	}}
	openConfig, err := decode([]byte(`<interfaces><interface><name>Port-channel42</name><state><oper-status>UP</oper-status></state><aggregation><state><member>GigabitEthernet2/0/7</member><member>GigabitEthernet2/0/8</member></state></aggregation></interface></interfaces>`))
	if err != nil {
		t.Fatal(err)
	}
	interfaceData, err := decode([]byte(`<interfaces><interface><name>Port-channel42</name><statistics><in-octets>1200</in-octets><out-octets>3400</out-octets><in-unicast-pkts>10</in-unicast-pkts><in-multicast-pkts>2</in-multicast-pkts><in-broadcast-pkts>3</in-broadcast-pkts><out-unicast-pkts>20</out-unicast-pkts><out-multicast-pkts>4</out-multicast-pkts><out-broadcast-pkts>5</out-broadcast-pkts><in-errors>6</in-errors><out-errors>7</out-errors><in-discards>8</in-discards><out-discards>9</out-discards></statistics></interface></interfaces>`))
	if err != nil {
		t.Fatal(err)
	}
	addAggregations(snapshot, openConfig, interfaceData)
	if len(snapshot.Aggregations) != 1 {
		t.Fatalf("aggregations=%+v", snapshot.Aggregations)
	}
	group := snapshot.Aggregations[0]
	if !group.HasCounters || group.Counters.RxBytes != 1200 || group.Counters.TxBytes != 3400 || group.Counters.RxPackets != 15 || group.Counters.TxPackets != 29 || group.Counters.RxErrors != 6 || group.Counters.TxErrors != 7 || group.Counters.RxDropped != 8 || group.Counters.TxDropped != 9 {
		t.Fatalf("aggregate counters=%+v", group)
	}
}
