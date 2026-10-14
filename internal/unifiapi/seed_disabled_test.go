package unifiapi

import (
	"testing"

	"github.com/TechBlueprints/disunified/internal/devicemodel"
)

func TestDisabledPresentPortIsNotReseeded(t *testing.T) {
	snapshot := &devicemodel.Snapshot{Ports: []devicemodel.Port{{Index: 6, IfName: "port6", Present: true, Enabled: false}}}
	isDefault := func(_ int, name string) bool { return name == "port6" }
	existing := []map[string]any{{"port_idx": 6, "name": "port6"}}
	seeded, count, _ := seedOverrides(snapshot, nil, existing, isDefault)
	if count != 1 {
		t.Fatalf("initial seed count=%d, want 1", count)
	}
	if _, count, _ := seedOverrides(snapshot, nil, seeded, isDefault); count != 0 {
		t.Fatalf("unchanged disabled port reseeded %d times", count)
	}
	snapshot.Ports[0].Enabled = true
	updated, count, _ := seedOverrides(snapshot, nil, seeded, isDefault)
	if count != 1 || isDisabledOverride(updated[0]) {
		t.Fatal("enabled port did not release its prior disabled seed")
	}
}
