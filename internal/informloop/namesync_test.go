package informloop

import (
	"context"
	"testing"
	"time"

	"github.com/TechBlueprints/disunified/internal/device"
	"github.com/TechBlueprints/disunified/internal/devicemodel"
	"github.com/jamesbraid/unifi-emu/inform"
)

func TestNameSyncPreservesOfflineEditsButAppliesFreshPushes(t *testing.T) {
	desc := inform.Descriptor{MAC: "02:00:00:00:00:01", Type: "usw", Ports: []inform.Port{{PortIdx: 6, IfName: "port6"}}}
	for _, pending := range []bool{false, true} {
		state := device.State{Adopted: true, Key: "0123456789abcdef0123456789abcdef", CfgVersion: "previous", SystemCfg: "switch.port.6.name=previous-name\nswitch.port.6.status=enabled\n"}
		if pending {
			state.PendingCfgVersion = "new"
			state.PendingSystemCfg = "switch.port.6.name=new-controller-name\nswitch.port.6.status=enabled\n"
		}
		session := device.NewSession(desc, "http://192.0.2.1:8080/inform", state, nil, time.Now())
		session.SetSnapshot(&devicemodel.Snapshot{Ports: []devicemodel.Port{{Index: 6, IfName: "port6", Present: true, Enabled: true, Description: "offline-device-edit"}}})
		controller := &freshController{}
		loop, err := New(desc, session, Config{Controller: controller, SyncNames: true, AllowInitialChanges: true})
		if err != nil {
			t.Fatal(err)
		}
		want := "offline-device-edit"
		if pending {
			loop.applyPending(context.Background())
			want = "new-controller-name"
		} else {
			loop.reconcile(context.Background())
		}
		if len(controller.applied) != 1 || controller.applied[0].Description != want {
			t.Fatalf("pending=%v applied=%v, want description %q", pending, controller.applied, want)
		}
	}
}
