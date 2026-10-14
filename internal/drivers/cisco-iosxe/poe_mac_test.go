package ciscoiosxe

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TechBlueprints/disunified/internal/devicemodel"
)

func TestCapturedPoEMeasurementsAndLearnedClients(t *testing.T) {
	_, _, snapshot := fixtureCollector(t)
	measured, attached := false, false
	for _, port := range snapshot.Ports {
		if port.PoE != nil && port.PoE.HasMetering && port.PoE.PowerW > 0 {
			measured = true
		}
		attached = attached || len(port.MACs) > 0
	}
	if !measured || snapshot.System.PoEBudgetW <= 0 || !attached || len(snapshot.MACTable) == 0 {
		t.Fatal("capture telemetry was not populated")
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "fixtures", "cisco-iosxe", "poe-oper.xml"))
	if err != nil {
		t.Fatal(err)
	}
	root, err := decode(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range root.all("poe-port-detail") {
		for _, port := range snapshot.Ports {
			if port.IfName == row.text("intf-name") && port.PoE != nil && row.has("power-consumption") && port.PoE.PowerW != row.number("power-consumption") {
				t.Fatal("allocated power replaced the consumption reading")
			}
		}
	}
}

func TestLearnedMACsMapAggregatesAndExcludeStaticEntries(t *testing.T) {
	snapshot := &devicemodel.Snapshot{Ports: []devicemodel.Port{{Index: 6, IfName: "GigabitEthernet2/0/6"}, {Index: 9, IfName: "GigabitEthernet2/0/9"}}, Aggregations: []devicemodel.Aggregation{{ID: 7, Name: "Port-channel7", Members: []devicemodel.AggregationMember{{PortIndex: 6}, {PortIndex: 9}}}}}
	root, err := decode([]byte(`<matm-oper-data><matm-mac-entry><mac>02:00:00:00:00:10</mac><vlan-id-number>20</vlan-id-number><mat-addr-type>dynamic</mat-addr-type><port>Po7</port></matm-mac-entry><matm-mac-entry><mac>02:00:00:00:00:11</mac><vlan-id-number>20</vlan-id-number><mat-addr-type>static</mat-addr-type><port>CPU</port></matm-mac-entry></matm-oper-data>`))
	if err != nil {
		t.Fatal(err)
	}
	addLearnedMACs(snapshot, root)
	if len(snapshot.MACTable) != 1 || snapshot.MACTable[0].PortIndex != 6 || len(snapshot.Ports[0].MACs) != 1 {
		t.Fatalf("learned entries=%v", snapshot.MACTable)
	}
}

type poeWriteTransport struct {
	after []byte
	edits []string
}

func (t *poeWriteTransport) Get(context.Context, string) ([]byte, error)       { return nil, nil }
func (t *poeWriteTransport) GetConfig(context.Context, string) ([]byte, error) { return t.after, nil }
func (t *poeWriteTransport) Edit(_ context.Context, config string) error {
	t.edits = append(t.edits, config)
	return nil
}
func (*poeWriteTransport) Close() error { return nil }

func TestPoEWriteReadbackAndIdempotence(t *testing.T) {
	transport := &poeWriteTransport{after: []byte(`<native><interface><GigabitEthernet><name>2/0/6</name><power><inline><never/></inline></power></GigabitEthernet></interface></native>`)}
	collector := NewCollector(transport)
	collector.last = &devicemodel.Snapshot{Ports: []devicemodel.Port{{Index: 6, IfName: "GigabitEthernet2/0/6", Present: true, Enabled: true, PoE: &devicemodel.PoE{Mode: "auto"}}}}
	root, err := decode([]byte(`<native><interface><GigabitEthernet><name>2/0/6</name></GigabitEthernet></interface></native>`))
	if err != nil {
		t.Fatal(err)
	}
	collector.native = nativePorts(root)
	desired := []devicemodel.PortDesired{{Index: 6, Enabled: true, PoEMode: "off"}}
	if count, err := collector.ApplyPorts(context.Background(), desired); err != nil || count != 1 {
		t.Fatalf("write=%d err=%v", count, err)
	}
	if !strings.Contains(transport.edits[0], "<never/>") {
		t.Fatal("inline power was not disabled")
	}
	if count, err := collector.ApplyPorts(context.Background(), desired); err != nil || count != 0 || len(transport.edits) != 1 {
		t.Fatal("PoE write was not idempotent")
	}
}
