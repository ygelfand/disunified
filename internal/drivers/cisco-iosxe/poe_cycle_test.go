package ciscoiosxe

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TechBlueprints/disunified/internal/devicemodel"
)

type cycleTransport struct {
	poeWriteTransport
	cancel         context.CancelFunc
	saves          int
	restoreFailure bool
}

func (t *cycleTransport) Edit(ctx context.Context, config string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	t.edits = append(t.edits, config)
	if strings.Contains(config, "<never/>") {
		t.after = []byte(`<native><interface><GigabitEthernet><name>2/0/6</name><power><inline><never/></inline></power></GigabitEthernet></interface></native>`)
		if t.cancel != nil {
			t.cancel()
		}
		return nil
	}
	if t.restoreFailure {
		return errors.New("restore failed")
	}
	t.after = []byte(`<native><interface><GigabitEthernet><name>2/0/6</name></GigabitEthernet></interface></native>`)
	if strings.Contains(config, "<max>12000</max>") {
		t.after = []byte(`<native><interface><GigabitEthernet><name>2/0/6</name><power><inline><auto><max>12000</max></auto></inline></power></GigabitEthernet></interface></native>`)
	}
	return nil
}

func (t *cycleTransport) RPC(_ context.Context, _ any, reply any) error {
	root, err := decode(t.after)
	if err != nil {
		return err
	}
	if nativePoEMode(nativePorts(root)["GigabitEthernet2/0/6"]) != "auto" {
		return errors.New("temporary off state was saved")
	}
	t.saves++
	reply.(*copyStartupReply).Result = "RPC request successful"
	return nil
}

func TestPoECycleRestoresAfterCancellationAndRestart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	transport := &cycleTransport{cancel: cancel, restoreFailure: true}
	collector := NewCollector(transport)
	collector.last = &devicemodel.Snapshot{System: devicemodel.System{MAC: "02:00:00:00:00:01"}, Ports: []devicemodel.Port{{Index: 6, IfName: "GigabitEthernet2/0/6", Present: true, PoE: &devicemodel.PoE{Mode: "auto"}}}}
	root, err := decode([]byte(`<native><interface><GigabitEthernet><name>2/0/6</name><power><inline><auto><max>12000</max></auto></inline></power></GigabitEthernet></interface></native>`))
	if err != nil {
		t.Fatal(err)
	}
	collector.native = nativePorts(root)
	collector.poeRestorePath = filepath.Join(t.TempDir(), "poe-restore.json")
	collector.saveConfig = true
	if err := collector.CyclePort(ctx, 6); err == nil {
		t.Fatal("failed restoration was ignored")
	}
	if transport.saves != 0 {
		t.Fatal("temporary off mode saved")
	}
	restarted := NewCollector(transport)
	restarted.last, restarted.native = collector.last, collector.native
	restarted.poeRestorePath = collector.poeRestorePath
	restarted.saveConfig = true
	if err := restarted.loadPoERestores(); err != nil {
		t.Fatal(err)
	}
	transport.restoreFailure = false
	if err := restarted.restorePoE(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(restarted.poeRestores) != 0 || transport.saves != 1 {
		t.Fatal("restore did not converge and save only auto mode")
	}
}
