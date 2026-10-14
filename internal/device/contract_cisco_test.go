package device

import (
	"context"
	"encoding/json"
	"maps"
	"path/filepath"
	"testing"
	"time"

	"github.com/TechBlueprints/disunified/internal/drivers/cisco-iosxe"
)

func TestWireContractCisco(t *testing.T) {
	c := ciscoiosxe.NewCollector(&ciscoiosxe.FixtureTransport{Directory: filepath.Join("..", "..", "docs", "fixtures", "cisco-iosxe")})
	snap, err := c.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	desc, err := DescriptorFor("UDC48X6", snap, Identity{MAC: snap.System.MAC, Serial: snap.System.Serial, IP: snap.System.Addresses[0].IP, Hostname: snap.System.Hostname, UDAPIVersion: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	desc.FWCaps = FWCapsFor(c.Capabilities())
	sess := NewSession(desc, "http://192.0.2.1:8080/inform", State{Key: "0123456789abcdef0123456789abcdef", Adopted: true, UseAESGCM: true}, nil, time.Now())
	sess.SetCapabilities(c.Capabilities())
	sess.SetSnapshot(snap)
	sess.SetGatewayIP("192.0.2.2")
	var payload map[string]any
	if err := json.Unmarshal(sess.BuildPayload(time.Now()), &payload); err != nil {
		t.Fatal(err)
	}
	omit := maps.Clone(contractOmissions)
	if _, ok := payload["service_mac"]; ok {
		delete(omit, "service_mac")
	}
	maps.Copy(omit, switchOmissions)
	maps.Copy(omit, map[string]string{
		"root_switch":          "native per-VLAN STP root is not collected",
		"stp_priority":         "per-VLAN STP priority is not mapped to one bridge priority",
		"mac_table_capability": "no NETCONF hardware FDB capacity capture",
	})
	portOmit := maps.Clone(portOmissions)
	maps.Copy(portOmit, map[string]string{
		"fec":            "no FEC capture on this platform",
		"sfp_compliance": "the captured front-panel ports are copper",
		"sfp_found":      "the captured front-panel ports are copper",
		"sfp_part":       "the captured front-panel ports are copper",
		"sfp_rxfault":    "the captured front-panel ports are copper",
		"sfp_serial":     "the captured front-panel ports are copper",
		"sfp_txfault":    "the captured front-panel ports are copper",
		"sfp_vendor":     "the captured front-panel ports are copper",
	})
	for _, ref := range []string{"inform-ecs-aggregation-uswf066.json", "inform-usw-xg16-usxg.json"} {
		real := loadReference(t, ref)
		compareKeys(t, "cisco device", real, payload, omit)
		upOmit := maps.Clone(portOmit)
		upOmit["stp_role"] = "no per-VLAN STP role capture"
		compareKeys(t, "cisco uplink", uplinkPort(t, real), uplinkPort(t, payload), upOmit)
		compareKeys(t, "cisco copper down port", downPort(t, real, false), downPort(t, payload, false), portOmit)
	}
}
