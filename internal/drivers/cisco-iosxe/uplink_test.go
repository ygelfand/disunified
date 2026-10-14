package ciscoiosxe

import (
	"context"
	"testing"

	"github.com/TechBlueprints/disunified/internal/devicemodel"
)

func TestConfiguredAggregateUplinkOverridesLLDP(t *testing.T) {
	c, _, s := fixtureCollector(t)
	var channel string
	for _, p := range s.Ports {
		if p.LAG != "" {
			channel = p.LAG
			break
		}
	}
	if channel == "" {
		t.Fatal("capture has no port-channel members")
	}
	c.uplink = channel
	s, err := c.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	idx := s.UplinkPort()
	if idx == 0 || s.Ports[idx-1].LAG != channel {
		t.Fatalf("uplink %d does not belong to %s", idx, channel)
	}
	if s.UplinkHint != idx {
		t.Errorf("hint=%d uplink=%d", s.UplinkHint, idx)
	}
	for _, p := range s.Ports {
		if p.LAG == channel && p.Up && !s.Ports[idx-1].Up {
			t.Fatal("down aggregate member selected while an up member exists")
		}
	}
}

func TestRedundantUplinkCandidates(t *testing.T) {
	s := &devicemodel.Snapshot{Ports: []devicemodel.Port{
		{Index: 7, LAG: "Port-channel42", Up: false},
		{Index: 8, LAG: "Port-channel42", Up: false},
		{Index: 19, LAG: "Port-channel64", Up: true},
		{Index: 20, LAG: "Port-channel64", Up: true},
	}}
	idx, err := configuredUplink(s, "Po42, Port-channel64")
	if err != nil || idx != 19 {
		t.Fatalf("fallback=%d,%v, want 19", idx, err)
	}
	s.Ports[1].Up = true
	idx, err = configuredUplink(s, "Port-channel42,Port-channel64")
	if err != nil || idx != 8 {
		t.Fatalf("preferred=%d,%v, want 8", idx, err)
	}
	for i := range s.Ports {
		s.Ports[i].Up = false
	}
	idx, err = configuredUplink(s, "Port-channel42,Port-channel64")
	if err != nil || idx != 7 {
		t.Fatalf("all down=%d,%v, want first known 7", idx, err)
	}
	if _, err := configuredUplink(s, "Port-channel42,Port-channel999"); err == nil {
		t.Fatal("unknown secondary candidate accepted")
	}
}

func TestConfiguredPhysicalUplinkAndUnknownInterface(t *testing.T) {
	c, _, s := fixtureCollector(t)
	target := s.Ports[len(s.Ports)-1]
	c.uplink = target.IfName
	s, err := c.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.UplinkPort() != target.Index {
		t.Errorf("uplink=%d, want %d", s.UplinkPort(), target.Index)
	}
	c.uplink = "Port-channel999"
	if _, err := c.Collect(context.Background()); err == nil {
		t.Fatal("unknown explicit uplink accepted")
	}
}
