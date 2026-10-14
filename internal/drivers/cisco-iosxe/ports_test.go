package ciscoiosxe

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/TechBlueprints/disunified/internal/devicemodel"
	"github.com/TechBlueprints/disunified/internal/unifimodel"
)

func TestModuleMediaAndModelCapacity(t *testing.T) {
	for _, tc := range []struct {
		name  string
		count int
		media devicemodel.Media
	}{
		{"GigabitEthernet1/1/1", 4, devicemodel.MediaSFP},
		{"TenGigabitEthernet1/1/1", 2, devicemodel.MediaSFPPlus},
		{"TenGigabitEthernet1/1/1", 4, devicemodel.MediaSFPPlus},
		{"TenGigabitEthernet1/1/1", 8, devicemodel.MediaSFPPlus},
		{"FortyGigabitEthernet1/1/1", 2, devicemodel.MediaQSFPPlus},
	} {
		if got := portMedia(tc.name); got != tc.media {
			t.Errorf("portMedia(%q)=%q, want %q", tc.name, got, tc.media)
		}
		layout := unifimodel.Layout{Copper: 48}
		switch tc.media {
		case devicemodel.MediaSFP:
			layout.SFP = tc.count
		case devicemodel.MediaSFPPlus:
			layout.SFPP = tc.count
		case devicemodel.MediaQSFPPlus:
			layout.QSFP28 = tc.count
		}
		candidate, err := unifimodel.Suggest(layout)
		if err != nil {
			t.Fatal(err)
		}
		if candidate.Ports < layout.Total() {
			t.Errorf("layout %+v selected undersized profile %+v", layout, candidate)
		}
	}
}

func TestModuleSlotsSurviveRemovalRestartAndModeChange(t *testing.T) {
	_, _, base := fixtureCollector(t)
	c := &Collector{statePath: filepath.Join(t.TempDir(), "ports.json")}
	withModule := *base
	withModule.Ports = append(slices.Clone(base.Ports), devicemodel.Port{IfName: "GigabitEthernet1/1/1", Media: devicemodel.MediaSFP, Present: true})
	if err := c.assignPorts(&withModule); err != nil {
		t.Fatal(err)
	}
	index := withModule.Ports[len(withModule.Ports)-1].Index
	removed := *base
	removed.Ports = slices.Clone(base.Ports)
	if err := c.assignPorts(&removed); err != nil {
		t.Fatal(err)
	}
	if len(removed.Ports) != len(withModule.Ports) || removed.Ports[index-1].Present {
		t.Fatalf("removed slot=%+v", removed.Ports[index-1])
	}
	restarted := &Collector{statePath: c.statePath}
	changed := *base
	changed.Ports = append(slices.Clone(base.Ports), devicemodel.Port{IfName: "TenGigabitEthernet1/1/1", Media: devicemodel.MediaSFPPlus, Present: true})
	if err := restarted.assignPorts(&changed); err != nil {
		t.Fatal(err)
	}
	if changed.Ports[index-1].IfName != "TenGigabitEthernet1/1/1" || !changed.Ports[index-1].Present {
		t.Fatalf("changed slot=%+v", changed.Ports[index-1])
	}
	for i, p := range base.Ports {
		if changed.Ports[i].Index != p.Index || changed.Ports[i].IfName != p.IfName {
			t.Errorf("front port moved: %+v", changed.Ports[i])
		}
	}
}

func TestDuplicateHardwareAliasesFailWithoutChangingSlots(t *testing.T) {
	c := &Collector{}
	s := &devicemodel.Snapshot{Ports: []devicemodel.Port{{IfName: "GigabitEthernet1/1/1", Present: true}, {IfName: "TenGigabitEthernet1/1/1", Present: true}}}
	if err := c.assignPorts(s); err == nil {
		t.Fatal("duplicate hardware aliases accepted")
	}
	if len(c.slots) != 0 {
		t.Fatal("failed discovery changed slot state")
	}
}
