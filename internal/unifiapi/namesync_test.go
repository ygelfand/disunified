package unifiapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/TechBlueprints/disunified/internal/devicemodel"
)

func TestNameSyncCommitsOnlySuccessfulWrites(t *testing.T) {
	controllerName := "first"
	fail := true
	writes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"_id": "device", "mac": "02:00:00:00:00:01", "name": "operator device", "port_overrides": []any{map[string]any{"port_idx": 6, "name": controllerName}}}}})
			return
		}
		writes++
		if fail {
			http.Error(w, "rejected", http.StatusBadRequest)
			return
		}
		var body struct {
			Overrides []map[string]any `json:"port_overrides"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		controllerName, _ = body.Overrides[0]["name"].(string)
		_, _ = w.Write([]byte(`{"meta":{"rc":"ok"},"data":[]}`))
	}))
	defer srv.Close()
	client := New(srv.URL, "test", "default", false)
	client.SyncNames = true
	client.NameStatePath = filepath.Join(t.TempDir(), "names.json")
	snapshot := &devicemodel.Snapshot{Ports: []devicemodel.Port{{Index: 6, IfName: "port6", Present: true, Description: "first"}}}
	provision := func() error {
		_, err := client.Provision(context.Background(), "02:00:00:00:00:01", snapshot, devicemodel.DefaultNamer{}, nil, func(int, string) bool { return false }, false)
		return err
	}
	if err := provision(); err != nil || writes != 0 {
		t.Fatalf("initial matching names: writes=%d err=%v", writes, err)
	}
	snapshot.Ports[0].Description = "second"
	if err := provision(); err == nil || client.nameState[6].Device != "first" {
		t.Fatal("failed update advanced observations")
	}
	fail = false
	if err := provision(); err != nil || controllerName != "second" {
		t.Fatalf("retry: name=%s err=%v", controllerName, err)
	}
	if err := provision(); err != nil || writes != 2 {
		t.Fatalf("echo caused another update: writes=%d err=%v", writes, err)
	}
	controllerName = "operator edit"
	if err := provision(); err != nil || writes != 2 {
		t.Fatalf("controller edit was overwritten: writes=%d err=%v", writes, err)
	}
}

func TestNamesFollowObservedChangesWithoutEcho(t *testing.T) {
	port := devicemodel.Port{Index: 6, IfName: "port6", Present: true}
	var previous nameObservation
	for _, step := range []struct {
		name, device, controller string
		isDefault, write         bool
	}{
		{name: "seed default", device: "first", controller: "Port 6", isDefault: true, write: true},
		{name: "API echo", device: "first", controller: "first"},
		{name: "device edit", device: "second", controller: "first", write: true},
		{name: "controller edit observed after device edit", device: "third", controller: "operator"},
		{name: "controller push applied", device: "operator", controller: "operator"},
		{name: "unchanged", device: "operator", controller: "operator"},
		{name: "later device edit", device: "fourth", controller: "operator", write: true},
	} {
		t.Run(step.name, func(t *testing.T) {
			next, write := nextNameObservation(previous, port, step.device, step.controller, step.isDefault)
			if write != step.write {
				t.Fatalf("API write=%v, want %v", write, step.write)
			}
			previous = next
		})
	}
}

func TestNameObservationsSurviveRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "names.json")
	client := &Client{NameStatePath: path}
	if err := client.loadNameState(); err != nil {
		t.Fatal(err)
	}
	port := devicemodel.Port{Index: 6, IfName: "port6"}
	state := map[int]nameObservation{6: {Interface: port.IfName, Device: "first", Controller: "first"}}
	if err := client.saveNameState(state); err != nil {
		t.Fatal(err)
	}
	restarted := &Client{NameStatePath: path}
	if err := restarted.loadNameState(); err != nil {
		t.Fatal(err)
	}
	if _, write := nextNameObservation(restarted.nameState[6], port, "offline-device-edit", "first", false); !write {
		t.Fatal("device edit while stopped was lost")
	}
	if _, write := nextNameObservation(restarted.nameState[6], port, "first", "offline-controller-edit", false); write {
		t.Fatal("controller edit while stopped would be overwritten")
	}
	port.IfName = "replacement"
	if _, write := nextNameObservation(restarted.nameState[6], port, "replacement-name", "operator", false); write {
		t.Fatal("replacement interface inherited previous ownership")
	}
}
