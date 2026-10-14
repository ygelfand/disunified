package ciscoiosxe

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/TechBlueprints/disunified/internal/devicemodel"
)

// Transport is the driver's fixture seam; filters contain only vendor YANG.
type Transport interface {
	Get(context.Context, string) ([]byte, error)
	GetConfig(context.Context, string) ([]byte, error)
	Edit(context.Context, string) error
	Close() error
}

type query struct {
	name, filter string
	config       bool
}

const nativeNS = "http://cisco.com/ns/yang/Cisco-IOS-XE-native"

const configFilter = `<native xmlns="` + nativeNS + `"><hostname/><version/><interface/><spanning-tree/></native>`

func operationalFilter() string {
	var filter strings.Builder
	for _, q := range queries() {
		if !q.config {
			filter.WriteString(q.filter)
		}
	}
	return filter.String()
}

func queries() []query {
	return []query{
		{"identity", `<native xmlns="` + nativeNS + `"><hostname/><version/></native>`, true},
		{"native-interfaces", `<native xmlns="` + nativeNS + `"><interface/></native>`, true},
		{"spanning-tree", `<native xmlns="` + nativeNS + `"><spanning-tree/></native>`, true},
		{"Cisco-IOS-XE-interfaces-oper", `<interfaces xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-interfaces-oper"/>`, false},
		{"Cisco-IOS-XE-device-hardware-oper", `<device-hardware-data xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-device-hardware-oper"/>`, false},
		{"Cisco-IOS-XE-platform-oper", `<components xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-platform-oper"/>`, false},
		{"Cisco-IOS-XE-lldp-oper", `<lldp-entries xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-lldp-oper"/>`, false},
		{"Cisco-IOS-XE-cdp-oper", `<cdp-neighbor-details xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-cdp-oper"/>`, false},
		{"Cisco-IOS-XE-arp-oper", `<arp-data xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-arp-oper"/>`, false},
		{"Cisco-IOS-XE-vlan-oper", `<vlans xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-vlan-oper"/>`, false},
		{"poe-oper", `<poe-oper-data xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-poe-oper"/>`, false},
		{"matm-oper", `<matm-oper-data xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-matm-oper"/>`, false},
		{"Cisco-IOS-XE-memory-oper", `<memory-statistics xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-memory-oper"/>`, false},
		{"Cisco-IOS-XE-process-cpu-oper", `<cpu-usage xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-process-cpu-oper"><cpu-utilization><five-seconds/><one-minute/><five-minutes/></cpu-utilization></cpu-usage>`, false},
		{"openconfig-interfaces", `<interfaces xmlns="http://openconfig.net/yang/interfaces"><interface><name/><state><oper-status/></state><aggregation xmlns="http://openconfig.net/yang/interfaces/aggregate"><state/></aggregation></interface></interfaces>`, false},
	}
}

// Collector maps IOS XE state to the shared model and applies bounded diffs.
type Collector struct {
	mu             sync.Mutex
	t              Transport
	last           *devicemodel.Snapshot
	native         map[string]node
	statePath      string
	slots          []portSlot
	slotsLoaded    bool
	module         moduleOptions
	uplink         string
	switching      bool
	saveConfig     bool
	savePending    bool
	savePath       string
	poeRestorePath string
	poeRestores    map[string]poeRestore
}

// NewCollector wraps a NETCONF transport or capture transport.
func NewCollector(t Transport) *Collector { return &Collector{t: t} }

// Start runs the complete collection and rejects missing identity or ports.
func (c *Collector) Start(ctx context.Context) (*devicemodel.Snapshot, error) { return c.Collect(ctx) }
func (c *Collector) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t.Close()
}

// Capabilities declares the optional controls supported by this driver.
func (c *Collector) Capabilities() devicemodel.Capabilities {
	c.mu.Lock()
	defer c.mu.Unlock()
	capabilities := devicemodel.Capabilities{LAGReporting: true, LACP: true, BPDUGuard: true}
	if c.last != nil {
		for _, port := range c.last.Ports {
			capabilities.PoE = capabilities.PoE || port.PoE != nil
		}
	}
	return capabilities
}

// Collect requires a complete poll before replacing the last known state.
func (c *Collector) Collect(ctx context.Context) (*devicemodel.Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	config, err := c.t.GetConfig(ctx, configFilter)
	if err != nil {
		return nil, fmt.Errorf("cisco-iosxe configuration: %w", err)
	}
	native, err := decode(config)
	if err != nil {
		return nil, fmt.Errorf("cisco-iosxe configuration XML: %w", err)
	}
	filter := operationalFilter()
	if capabilities, ok := c.t.(interface{ SupportsModule(string) bool }); ok {
		var selected strings.Builder
		for _, q := range queries() {
			if q.config {
				continue
			}
			if q.name == "poe-oper" && !capabilities.SupportsModule("Cisco-IOS-XE-poe-oper") {
				continue
			}
			if q.name == "matm-oper" && !capabilities.SupportsModule("Cisco-IOS-XE-matm-oper") {
				continue
			}
			selected.WriteString(q.filter)
		}
		filter = selected.String()
	}
	oper, err := c.t.Get(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("cisco-iosxe operational data: %w", err)
	}
	state, err := decode(oper)
	if err != nil {
		return nil, fmt.Errorf("cisco-iosxe operational XML: %w", err)
	}
	data := map[string]node{}
	for _, q := range queries() {
		if q.config {
			data[q.name] = native
			continue
		}
		part := node{}
		namespace := "http://cisco.com/ns/yang/" + q.name
		if q.name == "openconfig-interfaces" {
			namespace = "http://openconfig.net/yang/interfaces"
		}
		if q.name == "poe-oper" {
			namespace = "http://cisco.com/ns/yang/Cisco-IOS-XE-poe-oper"
		}
		if q.name == "matm-oper" {
			namespace = "http://cisco.com/ns/yang/Cisco-IOS-XE-matm-oper"
		}
		for _, child := range state.Children {
			if child.XMLName.Space == namespace {
				part.Children = append(part.Children, child)
			}
		}
		data[q.name] = part
	}
	if capabilities, ok := c.t.(interface{ SupportsFeature(string, string) bool }); ok {
		c.switching = capabilities.SupportsFeature("Cisco-IOS-XE-features", "switching-platform")
	}
	snap, ports, err := build(data, time.Now(), c.module, c.switching)
	if err != nil {
		return nil, err
	}
	if err := c.assignPorts(snap); err != nil {
		return nil, err
	}
	addAggregations(snap, data["openconfig-interfaces"], data["Cisco-IOS-XE-interfaces-oper"])
	addPoE(snap, ports, data["poe-oper"], data["Cisco-IOS-XE-platform-oper"])
	addLearnedMACs(snap, data["matm-oper"])
	if c.uplink != "" {
		idx, err := configuredUplink(snap, c.uplink)
		if err != nil {
			return nil, err
		}
		snap.UplinkHint = idx
	}
	c.last, c.native = snap, ports
	if len(c.poeRestores) > 0 {
		if err := c.restorePoE(ctx); err != nil {
			return nil, err
		}
	}
	return snap, nil
}
