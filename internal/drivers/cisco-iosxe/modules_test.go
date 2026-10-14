package ciscoiosxe

import (
	"slices"
	"testing"

	"github.com/TechBlueprints/disunified/internal/devicemodel"
)

func TestModuleInterfaceFamilies(t *testing.T) {
	for _, tc := range []struct {
		model string
		names []string
	}{
		{"C3850-NM-4-1G", []string{"GigabitEthernet1/1/1", "GigabitEthernet1/1/2", "GigabitEthernet1/1/3", "GigabitEthernet1/1/4"}},
		{"C3850-NM-2-10G", []string{"GigabitEthernet1/1/1", "GigabitEthernet1/1/2", "TenGigabitEthernet1/1/3", "TenGigabitEthernet1/1/4"}},
		{"C3850-NM-4-10G", []string{"TenGigabitEthernet1/1/1", "TenGigabitEthernet1/1/2", "TenGigabitEthernet1/1/3", "TenGigabitEthernet1/1/4"}},
		{"C3850-NM-8-10G", []string{"TenGigabitEthernet1/1/1", "TenGigabitEthernet1/1/2", "TenGigabitEthernet1/1/3", "TenGigabitEthernet1/1/4", "TenGigabitEthernet1/1/5", "TenGigabitEthernet1/1/6", "TenGigabitEthernet1/1/7", "TenGigabitEthernet1/1/8"}},
		{"C3850-NM-2-40G", []string{"FortyGigabitEthernet1/1/1", "FortyGigabitEthernet1/1/2"}},
	} {
		if got := moduleInterfaces(tc.model, "1/1", "native", nil); !slices.Equal(got, tc.names) {
			t.Errorf("moduleInterfaces(%s)=%v, want %v", tc.model, got, tc.names)
		}
	}
}

func TestBreakoutLanesFoldIntoPhysicalCages(t *testing.T) {
	modules := map[string]string{"1/1": "C3850-NM-2-40G"}
	var lanes []devicemodel.Port
	for _, name := range moduleInterfaces(modules["1/1"], "1/1", "breakout", nil) {
		lanes = append(lanes, devicemodel.Port{IfName: name, Present: true, Enabled: true, Up: true, SpeedMbps: 10000, Counters: devicemodel.Counters{RxBytes: 10}})
	}
	ports := foldModuleLanes(lanes, modules)
	if len(ports) != 2 {
		t.Fatalf("cages=%d, want 2", len(ports))
	}
	for _, p := range ports {
		if p.Lanes != 4 || len(p.Interfaces) != 4 || p.Media != devicemodel.MediaQSFPPlus || p.Counters.RxBytes != 40 || !p.Up {
			t.Errorf("folded cage=%+v", p)
		}
	}
}

func TestModuleOverrideFromCapturedInterfaceAliases(t *testing.T) {
	c, _, _ := fixtureCollector(t)
	for _, model := range []string{"C3850-NM-4-1G", "C3850-NM-2-10G", "C3850-NM-4-10G", "C3850-NM-8-10G", "C3850-NM-2-40G"} {
		found, err := discoverModules(node{}, node{}, c.native, moduleOptions{model: model, mode: "native"})
		if err != nil {
			t.Fatal(err)
		}
		if found["1/1"] != model {
			t.Errorf("override=%v", found)
		}
		for _, name := range moduleInterfaces(model, "1/1", "native", nil) {
			if c.native[name].XMLName.Local == "" {
				t.Errorf("captured alias missing: %s", name)
			}
		}
	}
}

func TestModuleOptionsRejectUnknownLayout(t *testing.T) {
	for _, options := range []map[string]string{{"uplink_module": "invented"}, {"uplink_mode": "invented"}, {"uplink_module": "C3850-NM-4-1G", "uplink_mode": "breakout"}} {
		if _, err := parseModuleOptions(options); err == nil {
			t.Errorf("invalid options accepted: %v", options)
		}
	}
}
