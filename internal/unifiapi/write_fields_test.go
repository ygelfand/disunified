package unifiapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeviceWritesExcludeReadOnlyPortFields(t *testing.T) {
	var received struct {
		Ports []map[string]any `json:"port_overrides"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Error(err)
		}
		_, _ = w.Write([]byte(`{"meta":{"rc":"ok"},"data":[]}`))
	}))
	defer server.Close()
	port := map[string]any{"port_idx": 6, "name": "operator", "poe_mode": "off", "aggregate_members": []int{6, 9}, "lag_idx": 7, "port_poe": true}
	client := New(server.URL, "test", "default", false)
	if err := client.UpdateDevice(context.Background(), "device", map[string]any{"port_overrides": []map[string]any{port}}); err != nil {
		t.Fatal(err)
	}
	if len(received.Ports) != 1 {
		t.Fatal("port override lost")
	}
	got := received.Ports[0]
	if _, ok := got["lag_idx"]; ok {
		t.Fatal("read-only aggregation ID sent")
	}
	if _, ok := got["port_poe"]; ok {
		t.Fatal("read-only PoE capability sent")
	}
	if got["poe_mode"] != "off" || got["name"] != "operator" || !sameJSON(got["aggregate_members"], []int{6, 9}) {
		t.Fatal("writable settings changed")
	}
	if port["lag_idx"] != 7 || port["port_poe"] != true {
		t.Fatal("input record mutated")
	}
}
