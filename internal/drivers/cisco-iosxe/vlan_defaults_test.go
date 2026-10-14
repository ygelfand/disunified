package ciscoiosxe

import "testing"

func TestVLANDefaultsRespectPlatformAndRoutedConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, xml, mode string
		switching       bool
	}{
		{name: "switch defaults", xml: `<port/>`, switching: true, mode: "access"},
		{name: "router defaults", xml: `<port/>`},
		{name: "explicit routed", xml: `<port><switchport-conf><switchport>false</switchport></switchport-conf></port>`, switching: true},
		{name: "routed address", xml: `<port><ip><address><dhcp/></address></ip></port>`, switching: true},
		{name: "routed vrf", xml: `<port><vrf><forwarding>management</forwarding></vrf></port>`, switching: true},
		{name: "explicit switchport", xml: `<port><switchport-conf><switchport>true</switchport></switchport-conf></port>`, mode: "access"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, err := decode([]byte(tc.xml))
			if err != nil {
				t.Fatal(err)
			}
			v, err := vlanState(n.child("port"), tc.switching)
			if err != nil || v.Mode != tc.mode {
				t.Fatalf("VLAN=%+v err=%v, want mode %q", v, err, tc.mode)
			}
			if tc.mode == "access" && v.NativeVLAN != 1 {
				t.Fatal("missing default access VLAN")
			}
		})
	}
}
