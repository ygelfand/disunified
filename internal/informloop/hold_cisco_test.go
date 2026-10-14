package informloop

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/TechBlueprints/disunified/internal/drivers/cisco-iosxe"
)

func TestCiscoInitialControllerDefaultsAreHeld(t *testing.T) {
	f := &ciscoiosxe.FixtureTransport{Directory: filepath.Join("..", "..", "docs", "fixtures", "cisco-iosxe")}
	c := ciscoiosxe.NewCollector(f)
	if _, err := c.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(filepath.Join("..", "..", "docs", "fixtures", "controller-10.6.106", "system_cfg.txt"))
	if err != nil {
		t.Fatal(err)
	}
	l, _, _ := newPendingLoop(t, c, false, false)
	desired := l.desiredPorts(string(text))
	if len(c.PlanPorts(desired)) == 0 {
		t.Fatal("controller defaults unexpectedly match captured configuration")
	}
	if !l.holdInitialPush("capture", desired) {
		t.Fatal("initial controller defaults were not held")
	}
	if len(f.Edits) != 0 {
		t.Fatal("planning wrote configuration")
	}
}
