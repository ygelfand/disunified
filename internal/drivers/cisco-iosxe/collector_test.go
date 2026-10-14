package ciscoiosxe

import (
	"context"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TechBlueprints/disunified/internal/devicemodel"
)

func fixtureCollector(t *testing.T) (*Collector, *FixtureTransport, *devicemodel.Snapshot) {
	t.Helper()
	f := &FixtureTransport{Directory: filepath.Join("..", "..", "..", "docs", "fixtures", "cisco-iosxe")}
	c := NewCollector(f)
	s, err := c.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return c, f, s
}

func TestCapturedSwitch(t *testing.T) {
	_, _, s := fixtureCollector(t)
	if s.System.Model == "" || s.System.Version == "" || s.System.MAC == "" {
		t.Fatalf("identity=%+v", s.System)
	}
	if len(s.Ports) == 0 {
		t.Fatal("no physical ports collected")
	}
	var traffic, neighbors, vlans int
	seen := map[string]bool{}
	for i, p := range s.Ports {
		if p.Index != i+1 || p.IfName == "" || seen[p.IfName] {
			t.Errorf("port slots are not uniquely indexed: %+v", p)
		}
		seen[p.IfName] = true
		if p.Counters.RxBytes > 0 {
			traffic++
		}
		if p.Neighbor != nil {
			neighbors++
		}
	}
	for _, id := range s.VLANs {
		if id > 0 {
			vlans++
		}
	}
	if traffic == 0 || neighbors == 0 || vlans == 0 {
		t.Errorf("traffic=%d neighbors=%d VLANs=%d", traffic, neighbors, vlans)
	}
	if !s.System.HasTemperature || len(s.System.PSUs) == 0 || s.System.Uptime <= 0 || s.System.MemTotalKB == 0 {
		t.Errorf("health data missing: %+v", s.System)
	}
	if len(s.System.OOBInterfaces) == 0 {
		t.Errorf("OOB=%+v", s.System.OOBInterfaces)
	}
	if s.System.OOBInterfaces[0].IP == "0.0.0.0" {
		t.Error("unspecified address reported as an assigned OOB address")
	}
}

func TestDescriptionReadbackAndIdempotence(t *testing.T) {
	interfaceConfig := `<GigabitEthernet><name>2/0/7</name><description>before</description><switchport><access xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-switch"><vlan><vlan>1</vlan></vlan></access></switchport></GigabitEthernet>`
	port := devicemodel.Port{Present: true, Enabled: true, Description: "before", VLAN: devicemodel.PortVLAN{Mode: "access", NativeVLAN: 1}}
	c, f, config := newWritableCollector(t, interfaceConfig, port, []int{1})
	updated := strings.Replace(config, `<description>before</description>`, `<description>after</description>`, 1)
	f.NativeReadback = []byte(updated)
	d := devicemodel.PortDesired{Index: 1, Enabled: true, Description: "after"}
	if got := c.PlanPorts([]devicemodel.PortDesired{d}); len(got) != 1 || got[0] != 1 {
		t.Fatalf("plan=%v", got)
	}
	n, err := c.ApplyPorts(context.Background(), []devicemodel.PortDesired{d})
	if err != nil || n != 1 {
		t.Fatalf("apply=%d,%v", n, err)
	}
	if len(f.Edits) != 1 || !strings.Contains(f.Edits[0], "<name>2/0/7</name><description>after</description>") {
		t.Fatalf("edits=%v", f.Edits)
	}
	n, err = c.ApplyPorts(context.Background(), []devicemodel.PortDesired{d})
	if err != nil || n != 0 || len(f.Edits) != 1 {
		t.Fatalf("second apply=%d,%v edits=%d", n, err, len(f.Edits))
	}
	f.NativeReadback = []byte(strings.Replace(config, `<description>before</description>`, "", 1))
	d.Description = ""
	n, err = c.ApplyPorts(context.Background(), []devicemodel.PortDesired{d})
	if err != nil || n != 1 {
		t.Fatalf("restore=%d,%v", n, err)
	}
	if !strings.Contains(f.Edits[1], `nc:operation="remove"`) {
		t.Errorf("restore edit=%s", f.Edits[1])
	}
}

func TestUnconfirmedWriteDoesNotConverge(t *testing.T) {
	interfaceConfig := `<GigabitEthernet><name>2/0/7</name><description>before</description><switchport><access xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-switch"><vlan><vlan>1</vlan></vlan></access></switchport></GigabitEthernet>`
	port := devicemodel.Port{Present: true, Enabled: true, Description: "before", VLAN: devicemodel.PortVLAN{Mode: "access", NativeVLAN: 1}}
	c, f, _ := newWritableCollector(t, interfaceConfig, port, []int{1})
	d := devicemodel.PortDesired{Index: 1, Enabled: port.Enabled, Description: "after"}
	if _, err := c.ApplyPorts(context.Background(), []devicemodel.PortDesired{d}); err == nil {
		t.Fatal("unchanged readback accepted")
	}
	if got := c.PlanPorts([]devicemodel.PortDesired{d}); len(got) != 1 {
		t.Errorf("unconfirmed write marked converged: %v", got)
	}
	if len(f.Edits) != 1 {
		t.Errorf("writes=%d", len(f.Edits))
	}
}

func TestProfileOnlyPortDoesNotBlockWritablePorts(t *testing.T) {
	interfaceConfig := `<GigabitEthernet><name>2/0/7</name><description>before</description><switchport><access xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-switch"><vlan><vlan>1</vlan></vlan></access></switchport></GigabitEthernet>`
	port := devicemodel.Port{Present: true, Enabled: true, Description: "before", VLAN: devicemodel.PortVLAN{Mode: "access", NativeVLAN: 1}}
	c, f, config := newWritableCollector(t, interfaceConfig, port, []int{1})
	valid := devicemodel.PortDesired{Index: 1, Enabled: port.Enabled, Description: "after"}
	profileOnly := devicemodel.PortDesired{Index: 49, Enabled: false}
	f.NativeReadback = []byte(strings.Replace(config, `<description>before</description>`, `<description>after</description>`, 1))
	if changed, err := c.ApplyPorts(context.Background(), []devicemodel.PortDesired{valid, profileOnly}); err != nil || changed != 1 {
		t.Fatalf("valid port update blocked by profile-only index: changed=%d err=%v", changed, err)
	}
	if len(f.Edits) != 1 {
		t.Fatalf("writes=%v", f.Edits)
	}
	valid.VLANSet = true
	valid.NativeVLAN = 4094
	if _, err := c.ApplyPorts(context.Background(), []devicemodel.PortDesired{valid}); err == nil {
		t.Fatal("missing VLAN accepted")
	}
	if len(f.Edits) != 1 {
		t.Fatal("missing VLAN wrote another config")
	}
}

func TestMissingRequiredCaptureFails(t *testing.T) {
	f := &FixtureTransport{Directory: t.TempDir()}
	if _, err := NewCollector(f).Start(context.Background()); err == nil {
		t.Fatal("empty collector accepted")
	}
}

// changedCapture composes a target with configuration subtrees already
// observed elsewhere in the real capture; it does not invent protocol data.
func changedCapture(t *testing.T, f *FixtureTransport, name string, replace map[string]node) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.Directory, "native-interfaces.xml"))
	if err != nil {
		t.Fatal(err)
	}
	r, err := decode(b)
	if err != nil {
		t.Fatal(err)
	}
	ports := r.child("native").child("interface")
	for i, p := range ports.Children {
		if p.XMLName.Local+p.text("name") != name {
			continue
		}
		var children []node
		for _, n := range p.Children {
			if _, ok := replace[n.XMLName.Local]; !ok {
				children = append(children, n)
			}
		}
		for _, n := range replace {
			if n.XMLName.Local != "" {
				children = append(children, n)
			}
		}
		ports.Children[i].Children = children
	}
	b, err = xml.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestAdminStateDiff(t *testing.T) {
	c, f, s := fixtureCollector(t)
	var target devicemodel.Port
	var shutdown node
	for _, p := range s.Ports {
		if !p.Enabled {
			shutdown = c.native[p.IfName].child("shutdown")
		}
		if p.Enabled && target.IfName == "" {
			target = p
		}
	}
	if shutdown.XMLName.Local == "" || !target.Enabled {
		t.Fatal("capture lacks test preconditions")
	}
	f.NativeReadback = changedCapture(t, f, target.IfName, map[string]node{"shutdown": shutdown})
	d := devicemodel.PortDesired{Index: target.Index, Enabled: false, Description: target.Description}
	n, err := c.ApplyPorts(context.Background(), []devicemodel.PortDesired{d})
	if err != nil || n != 1 {
		t.Fatalf("shutdown=%d,%v", n, err)
	}
	if !strings.Contains(f.Edits[0], "<shutdown/>") {
		t.Errorf("shutdown edit=%s", f.Edits[0])
	}
	n, err = c.ApplyPorts(context.Background(), []devicemodel.PortDesired{d})
	if err != nil || n != 0 {
		t.Fatalf("repeat shutdown=%d,%v", n, err)
	}
	f.NativeReadback = changedCapture(t, f, target.IfName, map[string]node{"shutdown": {}})
	d.Enabled = true
	n, err = c.ApplyPorts(context.Background(), []devicemodel.PortDesired{d})
	if err != nil || n != 1 {
		t.Fatalf("no shutdown=%d,%v", n, err)
	}
	if !strings.Contains(f.Edits[1], `<shutdown xmlns:nc="urn:ietf:params:xml:ns:netconf:base:1.0" nc:operation="remove"/>`) {
		t.Errorf("no shutdown edit=%s", f.Edits[1])
	}
}

func TestExistingVLANMembership(t *testing.T) {
	c, f, s := fixtureCollector(t)
	target := s.Ports[0]
	var source devicemodel.Port
	for _, p := range s.Ports {
		if p.VLAN.Mode == "trunk" && !p.VLAN.AllowAll && p.LAG == "" {
			source = p
			break
		}
	}
	if source.IfName == "" {
		t.Fatal("capture lacks a trunk")
	}
	f.NativeReadback = changedCapture(t, f, target.IfName, map[string]node{"switchport": c.native[source.IfName].child("switchport")})
	d := devicemodel.PortDesired{Index: target.Index, Enabled: target.Enabled, Description: target.Description, VLANSet: true, NativeVLAN: source.VLAN.NativeVLAN, TaggedAll: source.VLAN.AllowAll, TaggedVLANs: source.VLAN.Allowed}
	n, err := c.ApplyPorts(context.Background(), []devicemodel.PortDesired{d})
	if err != nil || n != 1 {
		t.Fatalf("trunk apply=%d,%v", n, err)
	}
	if !strings.Contains(f.Edits[0], "<trunk/>") || !strings.Contains(f.Edits[0], `nc:operation="replace"`) {
		t.Errorf("trunk edit=%s", f.Edits[0])
	}
	n, err = c.ApplyPorts(context.Background(), []devicemodel.PortDesired{d})
	if err != nil || n != 0 {
		t.Fatalf("trunk repeat=%d,%v", n, err)
	}
}
