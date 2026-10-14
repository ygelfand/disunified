package ciscoiosxe

import (
	"context"
	"strings"
	"testing"

	"github.com/TechBlueprints/disunified/internal/devicemodel"
)

func TestApplyLAGMembershipAddAndRemove(t *testing.T) {
	interfaceConfig := `<GigabitEthernet><name>2/0/7</name><switchport><access xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-switch"><vlan><vlan>1</vlan></vlan></access></switchport></GigabitEthernet>`
	port := devicemodel.Port{Present: true, Enabled: true, VLAN: devicemodel.PortVLAN{Mode: "access", NativeVLAN: 1}}
	c, transport, config := newWritableCollector(t, interfaceConfig, port, []int{1})
	member := `<name>2/0/7</name><channel-group xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-ethernet"><number>42</number><mode>active</mode></channel-group>`
	transport.NativeReadback = []byte(strings.Replace(config, `<name>2/0/7</name>`, member, 1))
	desired := devicemodel.PortDesired{Index: 1, Enabled: true, LAG: 42}
	changed, err := c.ApplyPorts(context.Background(), []devicemodel.PortDesired{desired})
	if err != nil {
		t.Fatal(err)
	}
	if changed != 1 || !strings.Contains(transport.Edits[0], `<number>42</number><mode>active</mode>`) {
		t.Fatalf("add changed=%d edits=%v", changed, transport.Edits)
	}
	if c.last.Ports[0].LAGID != 42 || c.last.Ports[0].LAG != "Port-channel42" {
		t.Fatalf("added membership not committed: %+v", c.last.Ports[0])
	}

	transport.NativeReadback = []byte(config)
	desired.LAG = 0
	changed, err = c.ApplyPorts(context.Background(), []devicemodel.PortDesired{desired})
	if err != nil {
		t.Fatal(err)
	}
	if changed != 1 || !strings.Contains(transport.Edits[1], `nc:operation="remove"`) {
		t.Fatalf("remove changed=%d edits=%v", changed, transport.Edits)
	}
	if c.last.Ports[0].LAGID != 0 || c.last.Ports[0].LAG != "" {
		t.Fatalf("removed membership not committed: %+v", c.last.Ports[0])
	}
}

func TestApplyLAGMembershipMoveBetweenGroups(t *testing.T) {
	interfaceConfig := `<GigabitEthernet><name>2/0/7</name><channel-group xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-ethernet"><number>42</number><mode>active</mode></channel-group></GigabitEthernet><Port-channel><name>42</name><switchport><mode xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-switch"><trunk/></mode></switchport></Port-channel><Port-channel><name>64</name><switchport><mode xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-switch"><trunk/></mode></switchport></Port-channel>`
	port := devicemodel.Port{Index: 30, Present: true, Enabled: true, LAG: "Port-channel42", LAGID: 42, VLAN: devicemodel.PortVLAN{Mode: "trunk", NativeVLAN: 1, AllowAll: true}}
	c, transport, config := newWritableCollector(t, interfaceConfig, port, []int{1})
	c.last.Aggregations = []devicemodel.Aggregation{
		{ID: 64, Name: "Port-channel64", Members: []devicemodel.AggregationMember{{PortIndex: 20}, {PortIndex: 21}}},
		{ID: 42, Name: "Port-channel42", Members: []devicemodel.AggregationMember{{PortIndex: 30}, {PortIndex: 31}}},
	}
	old := `<channel-group xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-ethernet"><number>42</number><mode>active</mode></channel-group>`
	updated := `<channel-group xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-ethernet"><number>64</number><mode>active</mode></channel-group>`
	transport.NativeReadback = []byte(strings.Replace(config, old, updated, 1))
	desired := devicemodel.PortDesired{Index: 30, Enabled: true, LAG: 1}
	changed, err := c.ApplyPorts(context.Background(), []devicemodel.PortDesired{desired})
	if err != nil {
		t.Fatal(err)
	}
	if changed != 1 || !strings.Contains(transport.Edits[0], `<number>64</number><mode>active</mode>`) {
		t.Fatalf("move changed=%d edits=%v", changed, transport.Edits)
	}
	if c.last.Ports[0].LAGID != 64 || c.last.Ports[0].LAG != "Port-channel64" {
		t.Fatalf("moved membership not committed: %+v", c.last.Ports[0])
	}
}

func TestControllerLAGIndexDoesNotGetTreatedAsCiscoChannelGroup(t *testing.T) {
	interfaceConfig := `<GigabitEthernet><name>2/0/7</name><channel-group xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-ethernet"><number>64</number><mode>active</mode></channel-group></GigabitEthernet><Port-channel><name>42</name></Port-channel><Port-channel><name>64</name></Port-channel>`
	port := devicemodel.Port{Index: 20, Present: true, Enabled: true, LAG: "Port-channel64", LAGID: 64, VLAN: devicemodel.PortVLAN{Mode: "access", NativeVLAN: 1}}
	c, transport, _ := newWritableCollector(t, interfaceConfig, port, []int{1})
	c.last.Aggregations = []devicemodel.Aggregation{
		{ID: 64, Name: "Port-channel64", Members: []devicemodel.AggregationMember{{PortIndex: 20}, {PortIndex: 21}}},
		{ID: 42, Name: "Port-channel42", Members: []devicemodel.AggregationMember{{PortIndex: 30}, {PortIndex: 31}}},
	}
	desired := devicemodel.PortDesired{Index: 20, Enabled: true, LAG: 1}
	if got := c.PlanPorts([]devicemodel.PortDesired{desired}); len(got) != 0 {
		t.Fatalf("logical group mapping planned a false move: %v", got)
	}
	if changed, err := c.ApplyPorts(context.Background(), []devicemodel.PortDesired{desired}); err != nil || changed != 0 || len(transport.Edits) != 0 {
		t.Fatalf("no-op mapping changed=%d err=%v edits=%v", changed, err, transport.Edits)
	}
}

func TestApplyVLANToLogicalPortChannel(t *testing.T) {
	interfaceConfig := `<GigabitEthernet><name>2/0/7</name><channel-group xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-ethernet"><number>42</number><mode>active</mode></channel-group></GigabitEthernet><Port-channel><name>42</name><switchport><mode xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-switch"><trunk/></mode></switchport></Port-channel>`
	port := devicemodel.Port{Present: true, Enabled: true, LAG: "Port-channel42", LAGID: 42, VLAN: devicemodel.PortVLAN{Mode: "trunk", NativeVLAN: 1, AllowAll: true}}
	c, transport, config := newWritableCollector(t, interfaceConfig, port, []int{1, 3, 10})
	old := `<Port-channel><name>42</name><switchport><mode xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-switch"><trunk/></mode></switchport></Port-channel>`
	updated := `<Port-channel><name>42</name><switchport><mode xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-switch"><trunk/></mode><trunk xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-switch"><native><vlan>10</vlan></native><allowed><vlan><vlans>all</vlans></vlan></allowed></trunk></switchport></Port-channel>`
	transport.NativeReadback = []byte(strings.Replace(config, old, updated, 1))
	desired := devicemodel.PortDesired{Index: 1, Enabled: true, LAG: 1, VLANSet: true, NativeVLAN: 10, TaggedAll: true}
	changed, err := c.ApplyPorts(context.Background(), []devicemodel.PortDesired{desired})
	if err != nil {
		t.Fatal(err)
	}
	if changed != 1 || !strings.Contains(transport.Edits[0], `<Port-channel><name>42</name><switchport>`) {
		t.Fatalf("changed=%d edits=%v", changed, transport.Edits)
	}
	if got := c.last.Ports[0].VLAN; got.Mode != "trunk" || got.NativeVLAN != 10 || !got.AllowAll {
		t.Fatalf("port-channel VLAN not committed to member: %+v", got)
	}
}

func TestLAGCapabilityIsAdvertisedWithWriteSupport(t *testing.T) {
	interfaceConfig := `<GigabitEthernet><name>2/0/7</name><switchport><access xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-switch"><vlan><vlan>1</vlan></vlan></access></switchport></GigabitEthernet>`
	port := devicemodel.Port{Present: true, Enabled: true, VLAN: devicemodel.PortVLAN{Mode: "access", NativeVLAN: 1}}
	c, _, _ := newWritableCollector(t, interfaceConfig, port, []int{1})
	if !c.Capabilities().LACP {
		t.Fatal("LACP write capability not advertised")
	}
}
