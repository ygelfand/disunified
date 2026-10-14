package ciscoiosxe

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
)

// FixtureTransport serves sanitized device captures and records edit requests.
// Readback can be replaced with another real capture for write-path tests.
type FixtureTransport struct {
	Directory      string
	Edits          []string
	NativeReadback []byte
	VLANReadback   []byte
}

func (f *FixtureTransport) read(filter string) ([]byte, error) {
	if filter == `<vlans xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-vlan-oper"/>` && f.VLANReadback != nil {
		return f.VLANReadback, nil
	}
	if filter == configFilter || filter == operationalFilter() {
		merged := node{XMLName: xml.Name{Local: "data"}}
		var native node
		for _, q := range queries() {
			if q.config != (filter == configFilter) {
				continue
			}
			b, err := f.read(q.filter)
			if err != nil {
				return nil, err
			}
			n, err := decode(b)
			if err != nil {
				return nil, err
			}
			if q.config {
				native.Children = append(native.Children, n.child("native").Children...)
			} else {
				merged.Children = append(merged.Children, n.Children...)
			}
		}
		if filter == configFilter {
			native.XMLName = xml.Name{Space: nativeNS, Local: "native"}
			merged.Children = append(merged.Children, native)
		}
		return xml.Marshal(merged)
	}
	for _, q := range queries() {
		if q.filter == filter {
			if q.name == "native-interfaces" && f.NativeReadback != nil {
				return f.NativeReadback, nil
			}
			return os.ReadFile(filepath.Join(f.Directory, q.name+".xml"))
		}
	}
	return nil, fmt.Errorf("no fixture for filter %q", filter)
}
func (f *FixtureTransport) Get(_ context.Context, filter string) ([]byte, error) {
	return f.read(filter)
}
func (f *FixtureTransport) GetConfig(_ context.Context, filter string) ([]byte, error) {
	return f.read(filter)
}
func (f *FixtureTransport) Edit(_ context.Context, config string) error {
	f.Edits = append(f.Edits, config)
	return nil
}
func (f *FixtureTransport) Close() error { return nil }
