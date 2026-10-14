package informloop

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/TechBlueprints/disunified/internal/device"
	"github.com/TechBlueprints/disunified/internal/devicemodel"
)

type uplinkCollector struct{ snapshot *devicemodel.Snapshot }

func (c uplinkCollector) Collect(context.Context) (*devicemodel.Snapshot, error) {
	return c.snapshot, nil
}

func TestPinnedUplinkSurvivesCollection(t *testing.T) {
	s := &devicemodel.Snapshot{System: devicemodel.System{MAC: "02:00:00:00:00:01", Serial: "SSJ00000000"}, Ports: []devicemodel.Port{
		{Index: 1, Up: true, Neighbor: &devicemodel.Neighbor{IsBridge: true, IsRouter: true}},
		{Index: 3, Up: true, LAG: "bundle7", LAGID: 7},
		{Index: 5, Up: true, LAG: "bundle7", LAGID: 7},
	}, Aggregations: []devicemodel.Aggregation{{ID: 7, Name: "bundle7", Up: true, Members: []devicemodel.AggregationMember{{PortIndex: 3, Active: true, SpeedMbps: 1000}, {PortIndex: 5, Active: true, SpeedMbps: 1000}}}}}
	c := uplinkCollector{snapshot: s}
	desc, err := device.DescriptorFor("UDC48X6", s, device.Identity{MAC: s.System.MAC, Serial: s.System.Serial, IP: "192.0.2.9", UplinkPort: 5})
	if err != nil {
		t.Fatal(err)
	}
	sess := device.NewSession(desc, "http://192.0.2.1:8080/inform", device.State{Adopted: true, Key: "0123456789abcdef0123456789abcdef"}, nil, time.Now())
	l, err := New(desc, sess, Config{Collector: c, UplinkPort: 5})
	if err != nil {
		t.Fatal(err)
	}
	l.collect(context.Background())
	var payload map[string]any
	if err := json.Unmarshal(sess.BuildPayload(time.Now()), &payload); err != nil {
		t.Fatal(err)
	}
	for _, row := range payload["port_table"].([]any) {
		p := row.(map[string]any)
		if p["is_uplink"] == true && p["port_idx"] != float64(3) {
			t.Errorf("LLDP overwrote pinned uplink: %v", p["port_idx"])
		}
	}
	if l.uplinkPort != 5 {
		t.Errorf("loop uplink=%d, want 5", l.uplinkPort)
	}
}
