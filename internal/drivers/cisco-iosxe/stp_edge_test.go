package ciscoiosxe

import (
	"context"
	"strings"
	"testing"

	"github.com/TechBlueprints/disunified/internal/devicemodel"
)

func TestApplySTPEdgeMode(t *testing.T) {
	interfaceConfig := `<GigabitEthernet><name>2/0/7</name><switchport><access xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-switch"><vlan><vlan>1</vlan></vlan></access></switchport><spanning-tree xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-spanning-tree"><portfast/></spanning-tree></GigabitEthernet>`
	port := devicemodel.Port{Present: true, Enabled: true, VLAN: devicemodel.PortVLAN{Mode: "access", NativeVLAN: 1}}
	c, transport, config := newWritableCollector(t, interfaceConfig, port, []int{1})
	readback := strings.Replace(config, `<portfast/>`, `<portfast><edge/></portfast><bpdufilter>enable</bpdufilter>`, 1)
	transport.NativeReadback = []byte(readback)
	desired := devicemodel.PortDesired{Index: 1, Enabled: true, STPEdge: true}
	changed, err := c.ApplyPorts(context.Background(), []devicemodel.PortDesired{desired})
	if err != nil {
		t.Fatal(err)
	}
	if changed != 1 || !strings.Contains(transport.Edits[0], `<portfast xmlns:nc="urn:ietf:params:xml:ns:netconf:base:1.0" nc:operation="replace"><edge/></portfast>`) {
		t.Fatalf("changed=%d edits=%v", changed, transport.Edits)
	}
	if !c.last.Ports[0].STPEdge {
		t.Fatal("confirmed STP edge state was not committed")
	}
}
