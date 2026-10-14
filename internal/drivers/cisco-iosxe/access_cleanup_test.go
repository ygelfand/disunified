package ciscoiosxe

import (
	"encoding/xml"
	"io"
	"strings"
	"testing"

	"github.com/TechBlueprints/disunified/internal/devicemodel"
)

func TestAccessModeRemovesOnlyTrunkVLANSettings(t *testing.T) {
	edit, err := decode([]byte(vlanEdit(devicemodel.PortVLAN{Mode: "access", NativeVLAN: 20})))
	if err != nil {
		t.Fatal(err)
	}
	sw := edit.child("switchport")
	if sw.text("access", "vlan", "vlan") != "20" {
		t.Fatal("access VLAN lost")
	}
	decoder := xml.NewDecoder(strings.NewReader(vlanEdit(devicemodel.PortVLAN{Mode: "access", NativeVLAN: 20})))
	removals := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if start, ok := token.(xml.StartElement); ok {
			for _, attr := range start.Attr {
				if attr.Name.Space == "urn:ietf:params:xml:ns:netconf:base:1.0" && attr.Name.Local == "operation" && attr.Value == "remove" {
					removals++
				}
			}
		}
	}
	if removals != 2 {
		t.Fatalf("got %d removals, want trunk native and allowed VLAN settings", removals)
	}
	desired := devicemodel.PortDesired{VLANSet: true, NativeVLAN: 20}
	stale, err := decode([]byte(`<port><switchport><mode><access/></mode><trunk><native><vlan>20</vlan></native><allowed><vlan><vlans>20,30</vlans></vlan></allowed></trunk></switchport></port>`))
	if err != nil {
		t.Fatal(err)
	}
	if !accessTrunkCleanup(stale.child("port"), desired) {
		t.Fatal("stale settings were not detected after switching to access")
	}
	desired.TaggedAll = true
	if accessTrunkCleanup(stale.child("port"), desired) {
		t.Fatal("trunk mode scheduled access cleanup")
	}
}
