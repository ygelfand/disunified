package ciscoiosxe

import (
	"context"
	"strings"
	"testing"

	"github.com/TechBlueprints/disunified/internal/devicemodel"
)

func TestEnsureVLANsCreatesOnlyMissingAndVerifies(t *testing.T) {
	interfaceConfig := `<GigabitEthernet><name>2/0/7</name><switchport><access xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-switch"><vlan><vlan>1</vlan></vlan></access></switchport></GigabitEthernet>`
	port := devicemodel.Port{Present: true, Enabled: true, VLAN: devicemodel.PortVLAN{Mode: "access", NativeVLAN: 1}}
	c, transport, _ := newWritableCollector(t, interfaceConfig, port, []int{1})
	transport.VLANReadback = []byte(`<vlans xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-vlan-oper"><vlan><id>1</id></vlan><vlan><id>4093</id></vlan></vlans>`)

	created, err := c.EnsureVLANs(context.Background(), []int{4093, 1, 4093})
	if err != nil {
		t.Fatal(err)
	}
	if created != 1 || len(transport.Edits) != 1 {
		t.Fatalf("created=%d edits=%d", created, len(transport.Edits))
	}
	edit := transport.Edits[0]
	if !strings.Contains(edit, `<vlan-list><id>4093</id></vlan-list>`) || strings.Contains(edit, `<id>1</id>`) {
		t.Fatalf("unexpected VLAN edit %s", edit)
	}
	if !strings.Contains(edit, `xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-vlan"`) {
		t.Fatalf("VLAN edit lacks Cisco VLAN namespace: %s", edit)
	}
	if !containsVLAN(c.last.VLANs, 4093) {
		t.Fatalf("confirmed VLAN not added to snapshot: %v", c.last.VLANs)
	}
}

func TestEnsureVLANsRejectsUnconfirmedCreation(t *testing.T) {
	interfaceConfig := `<GigabitEthernet><name>2/0/7</name><switchport><access xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-switch"><vlan><vlan>1</vlan></vlan></access></switchport></GigabitEthernet>`
	port := devicemodel.Port{Present: true, Enabled: true, VLAN: devicemodel.PortVLAN{Mode: "access", NativeVLAN: 1}}
	c, transport, _ := newWritableCollector(t, interfaceConfig, port, []int{1})
	transport.VLANReadback = []byte(`<vlans xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-vlan-oper"><vlan><id>1</id></vlan></vlans>`)

	if _, err := c.EnsureVLANs(context.Background(), []int{4093}); err == nil {
		t.Fatal("missing VLAN readback accepted")
	}
	if containsVLAN(c.last.VLANs, 4093) {
		t.Fatalf("unconfirmed VLAN added to snapshot: %v", c.last.VLANs)
	}
}

func containsVLAN(ids []int, want int) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
