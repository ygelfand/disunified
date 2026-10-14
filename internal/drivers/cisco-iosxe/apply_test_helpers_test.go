package ciscoiosxe

import (
	"testing"

	"github.com/TechBlueprints/disunified/internal/devicemodel"
)

const testIfName = "GigabitEthernet2/0/7"

func newWritableCollector(t *testing.T, interfaceConfig string, port devicemodel.Port, vlans []int) (*Collector, *FixtureTransport, string) {
	t.Helper()
	config := `<native xmlns="` + nativeNS + `"><interface>` + interfaceConfig + `</interface></native>`
	root, err := decode([]byte(config))
	if err != nil {
		t.Fatal(err)
	}
	transport := &FixtureTransport{NativeReadback: []byte(config)}
	collector := NewCollector(transport)
	collector.native = nativePorts(root)
	collector.switching = true
	if port.Index == 0 {
		port.Index = 1
	}
	if port.IfName == "" {
		port.IfName = testIfName
	}
	if port.Interfaces == nil {
		port.Interfaces = []string{port.IfName}
	}
	if port.Media == devicemodel.MediaUnknown {
		port.Media = devicemodel.MediaCopper1G
	}
	if port.SpeedCaps == nil {
		port.SpeedCaps = speedCaps(port.Media)
	}
	collector.last = &devicemodel.Snapshot{
		System: devicemodel.System{MAC: "02:00:00:00:00:01"},
		Ports:  []devicemodel.Port{port},
		VLANs:  append([]int(nil), vlans...),
	}
	return collector, transport, config
}
