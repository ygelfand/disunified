package informloop

import (
	"context"
	"maps"
	"path/filepath"
	"slices"
	"testing"

	"github.com/TechBlueprints/disunified/internal/device"
	"github.com/TechBlueprints/disunified/internal/drivers/cisco-iosxe"
)

func TestReplayCiscoReadOnly(t *testing.T) {
	f := &ciscoiosxe.FixtureTransport{Directory: filepath.Join("..", "..", "docs", "fixtures", "cisco-iosxe")}
	c := ciscoiosxe.NewCollector(f)
	s, err := c.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	addresses := slices.Sorted(maps.Keys(s.System.ARP))
	if len(addresses) == 0 {
		t.Fatal("capture lacks ARP data")
	}
	runReplay(t, replayDriver{Name: "cisco-iosxe", Replies: "replies.ndjson", Collector: c, Snapshot: s, GatewayIP: addresses[0], Written: func() string { return "" },
		Check: func(t *testing.T, i int, _ replayRecord, _ *device.Session, _ *Loop, _ string) {
			if len(f.Edits) != 0 {
				t.Errorf("reply %d caused edits in read-only mode", i)
			}
		},
	})
}
