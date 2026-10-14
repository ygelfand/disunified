package ciscoiosxe

import (
	"context"
	"strings"
	"testing"

	"github.com/TechBlueprints/disunified/internal/devicemodel"
)

func TestApplySpeedAndRestoreAutonegotiation(t *testing.T) {
	interfaceConfig := `<GigabitEthernet><name>2/0/7</name><switchport><access xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-switch"><vlan><vlan>1</vlan></vlan></access></switchport></GigabitEthernet>`
	port := devicemodel.Port{Present: true, Enabled: true, VLAN: devicemodel.PortVLAN{Mode: "access", NativeVLAN: 1}}
	c, transport, config := newWritableCollector(t, interfaceConfig, port, []int{1})
	port = c.last.Ports[0]
	if len(port.SpeedCaps) != 3 || port.SpeedCaps[0] != 10 || port.SpeedCaps[2] != 1000 {
		t.Fatalf("copper speed capabilities=%v", port.SpeedCaps)
	}
	forced := strings.Replace(config, `<name>2/0/7</name>`, `<name>2/0/7</name><speed xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-ethernet"><value-100/></speed>`, 1)
	transport.NativeReadback = []byte(forced)
	desired := devicemodel.PortDesired{Index: port.Index, Enabled: port.Enabled, SpeedMbps: 100}
	changed, err := c.ApplyPorts(context.Background(), []devicemodel.PortDesired{desired})
	if err != nil {
		t.Fatal(err)
	}
	if changed != 1 || !strings.Contains(transport.Edits[0], `<value-100/>`) || c.last.Ports[0].AutoNeg || c.last.Ports[0].AdminSpeed != 100 {
		t.Fatalf("forced speed state=%+v edits=%v", c.last.Ports[0], transport.Edits)
	}

	transport.NativeReadback = []byte(config)
	desired.SpeedMbps = 0
	changed, err = c.ApplyPorts(context.Background(), []devicemodel.PortDesired{desired})
	if err != nil {
		t.Fatal(err)
	}
	if changed != 1 || !strings.Contains(transport.Edits[1], `<auto/>`) || !c.last.Ports[0].AutoNeg || c.last.Ports[0].AdminSpeed != 0 {
		t.Fatalf("auto speed state=%+v edits=%v", c.last.Ports[0], transport.Edits)
	}
}

func TestApplyRejectsUnsupportedSpeedBeforeWriting(t *testing.T) {
	interfaceConfig := `<GigabitEthernet><name>2/0/7</name><switchport><access xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-switch"><vlan><vlan>1</vlan></vlan></access></switchport></GigabitEthernet>`
	port := devicemodel.Port{Present: true, Enabled: true, VLAN: devicemodel.PortVLAN{Mode: "access", NativeVLAN: 1}}
	c, transport, _ := newWritableCollector(t, interfaceConfig, port, []int{1})
	port = c.last.Ports[0]
	_, err := c.ApplyPorts(context.Background(), []devicemodel.PortDesired{{Index: port.Index, Enabled: port.Enabled, SpeedMbps: 2500}})
	if err == nil || len(transport.Edits) != 0 {
		t.Fatalf("unsupported speed err=%v edits=%v", err, transport.Edits)
	}
}
