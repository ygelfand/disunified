package unificfg

import "testing"

func TestPoEModeIsExplicitAndPreservesUnsupportedRequests(t *testing.T) {
	for _, key := range []string{"poe", "poe.opmode", "poe.mode"} {
		cfg := Parse("switch.port.6." + key + "=off\n")
		if cfg.Ports[6].PoEMode != "off" {
			t.Fatalf("%s mode=%q", key, cfg.Ports[6].PoEMode)
		}
		if got := Parse("switch.port.6." + key + "=shutdown\n").Ports[6].PoEMode; got != "off" {
			t.Fatalf("%s shutdown mode=%q, want off", key, got)
		}
	}
	if got := Parse("switch.port.6.name=label\n").Ports[6].PoEMode; got != "" {
		t.Fatal("omitted power setting changed PoE")
	}
	if got := Parse("switch.port.6.poe.opmode=pasv24\n").Ports[6].PoEMode; got != "pasv24" {
		t.Fatal("unsupported mode was silently converted")
	}
}
