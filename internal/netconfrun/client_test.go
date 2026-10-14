package netconfrun

import "testing"

func TestConfigRejectsIncompleteAuthentication(t *testing.T) {
	for _, cfg := range []Config{{Transport: "udp", Address: "192.0.2.1:830"}, {Transport: "ssh", Address: "192.0.2.1:830"}, {Transport: "tls", Address: "192.0.2.1:6513"}, {Transport: "ssh", Username: "admin", Address: "192.0.2.1"}} {
		if _, err := New(cfg); err == nil {
			t.Errorf("New(%+v) accepted invalid config", cfg)
		}
	}
}
