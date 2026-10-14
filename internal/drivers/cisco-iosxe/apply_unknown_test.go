package ciscoiosxe

import (
	"testing"

	"github.com/TechBlueprints/disunified/internal/devicemodel"
)

func TestUnknownVLANPreservesModeDuringRename(t *testing.T) {
	port := devicemodel.Port{Index: 6, Enabled: true, Description: "before"}
	desired := devicemodel.PortDesired{Index: 6, Enabled: true, Description: "after", VLANSet: true, NativeVLAN: 1, TaggedAll: true}
	if !portDiff(port, desired) || preserveUnknownVLAN(port, desired).VLANSet {
		t.Fatal("rename must remain actionable without a default VLAN write")
	}
	port.Description = "after"
	if portDiff(port, desired) {
		t.Fatal("default VLAN bundle caused a repeated change after rename")
	}
	desired.NativeVLAN = 20
	if !preserveUnknownVLAN(port, desired).VLANSet {
		t.Fatal("non-default VLAN request was silently discarded")
	}
	port.VLAN.Mode = "access"
	desired.NativeVLAN = 1
	if !preserveUnknownVLAN(port, desired).VLANSet {
		t.Fatal("known switchport mode lost its VLAN request")
	}
}
