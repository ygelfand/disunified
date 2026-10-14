package ciscoiosxe

import (
	"context"
	"strings"
	"testing"

	"github.com/TechBlueprints/disunified/internal/devicemodel"
)

func TestApplyBPDUGuardEnable(t *testing.T) {
	interfaceConfig := `<GigabitEthernet><name>2/0/7</name><switchport><access xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-switch"><vlan><vlan>1</vlan></vlan></access></switchport></GigabitEthernet>`
	port := devicemodel.Port{Present: true, Enabled: true, VLAN: devicemodel.PortVLAN{Mode: "access", NativeVLAN: 1}}
	c, transport, config := newWritableCollector(t, interfaceConfig, port, []int{1})
	transport.NativeReadback = []byte(strings.Replace(config, `</switchport>`, `</switchport><spanning-tree xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-spanning-tree"><bpduguard><enable/></bpduguard></spanning-tree>`, 1))
	desired := devicemodel.PortDesired{Index: 1, Enabled: true, BPDUGuard: true}
	changed, err := c.ApplyPorts(context.Background(), []devicemodel.PortDesired{desired})
	if err != nil {
		t.Fatal(err)
	}
	if changed != 1 || !strings.Contains(transport.Edits[0], `<bpduguard xmlns:nc="urn:ietf:params:xml:ns:netconf:base:1.0" nc:operation="replace"><enable/></bpduguard>`) {
		t.Fatalf("changed=%d edits=%v", changed, transport.Edits)
	}
	if !c.last.Ports[0].BPDUGuard {
		t.Fatal("confirmed BPDU guard state was not committed")
	}
	if !c.Capabilities().BPDUGuard {
		t.Fatal("BPDU guard capability not advertised")
	}
}
