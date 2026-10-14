package ciscoiosxe

import (
	"encoding/xml"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/TechBlueprints/disunified/internal/devicemodel"
)

type node struct {
	XMLName  xml.Name
	Text     string `xml:",chardata"`
	Children []node `xml:",any"`
}

func decode(b []byte) (node, error) {
	var n node
	s := strings.TrimSpace(string(b))
	if strings.HasPrefix(s, "<?xml") {
		if i := strings.Index(s, "?>"); i >= 0 {
			s = s[i+2:]
		}
	}
	err := xml.Unmarshal([]byte("<data>"+s+"</data>"), &n)
	if len(n.Children) == 1 && n.Children[0].XMLName.Local == "data" {
		n = n.Children[0]
	}
	return n, err
}
func (n node) child(name string) node {
	for _, c := range n.Children {
		if c.XMLName.Local == name {
			return c
		}
	}
	return node{}
}
func (n node) text(path ...string) string {
	for _, p := range path {
		n = n.child(p)
	}
	return strings.TrimSpace(n.Text)
}
func (n node) has(name string) bool { return n.child(name).XMLName.Local != "" }
func (n node) all(name string) []node {
	var out []node
	for _, c := range n.Children {
		if c.XMLName.Local == name {
			out = append(out, c)
		}
		out = append(out, c.all(name)...)
	}
	return out
}
func (n node) uint(path ...string) uint64 {
	v, _ := strconv.ParseUint(n.text(path...), 10, 64)
	return v
}
func (n node) number(path ...string) float64 {
	v, _ := strconv.ParseFloat(n.text(path...), 64)
	return v
}

func nativePorts(n node) map[string]node {
	out := map[string]node{}
	for _, family := range []string{"GigabitEthernet", "TenGigabitEthernet", "FortyGigabitEthernet", "TwentyFiveGigE", "HundredGigE", "Port-channel"} {
		for _, p := range n.all(family) {
			out[family+p.text("name")] = p
		}
	}
	return out
}

func canonicalInterface(n string) string {
	for _, pair := range [][2]string{{"Gi", "GigabitEthernet"}, {"Te", "TenGigabitEthernet"}, {"Fo", "FortyGigabitEthernet"}, {"Po", "Port-channel"}} {
		if strings.HasPrefix(n, pair[0]) && len(n) > len(pair[0]) && n[len(pair[0])] >= '0' && n[len(pair[0])] <= '9' {
			return pair[1] + n[len(pair[0]):]
		}
	}
	return n
}

func normalizeMAC(s string) string {
	s = strings.ToLower(strings.NewReplacer(":", "", ".", "", "-", "").Replace(s))
	if len(s) != 12 {
		return ""
	}
	b, err := strconv.ParseUint(s, 16, 64)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", b>>40&255, b>>32&255, b>>24&255, b>>16&255, b>>8&255, b&255)
}

func vlanState(p node, switching bool) (devicemodel.PortVLAN, error) {
	sw := p.child("switchport")
	v := devicemodel.PortVLAN{}
	if p.text("switchport-conf", "switchport") == "false" {
		return v, nil
	}
	if sw.XMLName.Local == "" {
		// On switching platforms, omitted switchport config is a default
		// Layer 2 port. IP/VRF configuration still identifies routed ports.
		layer2 := switching || p.text("switchport-conf", "switchport") == "true"
		if !layer2 || p.has("vrf") || p.child("ip").has("address") || p.child("ipv6").has("address") {
			return v, nil
		}
	}
	v.Mode = "access"
	v.NativeVLAN = 1
	mode := sw.child("mode")
	if mode.has("trunk") {
		v.Mode = "trunk"
		v.AllowAll = true
		trunk := sw.child("trunk")
		if n := trunk.uint("native", "vlan"); n > 0 {
			v.NativeVLAN = int(n)
		}
		allowed := trunk.text("allowed", "vlan", "vlans")
		if allowed != "" && allowed != "all" {
			ids, err := vlanList(allowed)
			if err != nil {
				return v, err
			}
			v.AllowAll = false
			v.Allowed = ids
		}
	} else if n := sw.uint("access", "vlan", "vlan"); n > 0 {
		v.NativeVLAN = int(n)
	}
	return v, nil
}

func vlanList(s string) ([]int, error) {
	set := map[int]bool{}
	if s == "none" {
		return nil, nil
	}
	for _, part := range strings.Split(s, ",") {
		a, b, rangeID := strings.Cut(strings.TrimSpace(part), "-")
		lo, err := strconv.Atoi(a)
		if err != nil {
			return nil, err
		}
		hi := lo
		if rangeID {
			hi, err = strconv.Atoi(b)
			if err != nil {
				return nil, err
			}
		}
		if lo < 1 || hi > 4094 || lo > hi {
			return nil, fmt.Errorf("invalid VLAN range %q", part)
		}
		for n := lo; n <= hi; n++ {
			set[n] = true
		}
	}
	out := make([]int, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Ints(out)
	return out, nil
}

func portOrder(name string) []int {
	i := strings.IndexAny(name, "0123456789")
	if i < 0 {
		return nil
	}
	var ns []int
	for _, s := range strings.Split(name[i:], "/") {
		n, err := strconv.Atoi(s)
		if err != nil {
			return nil
		}
		ns = append(ns, n)
	}
	return ns
}

func portMedia(name string) devicemodel.Media {
	order := portOrder(name)
	if len(order) != 3 {
		return devicemodel.MediaUnknown
	}
	if order[1] == 0 {
		if strings.HasPrefix(name, "TenGigabitEthernet") {
			return devicemodel.MediaCopper10G
		}
		return devicemodel.MediaCopper1G
	}
	switch {
	case strings.HasPrefix(name, "FortyGigabitEthernet"):
		return devicemodel.MediaQSFPPlus
	case strings.HasPrefix(name, "HundredGigE"):
		return devicemodel.MediaQSFP28
	case strings.HasPrefix(name, "TwentyFiveGigE"):
		return devicemodel.MediaSFP28
	case strings.HasPrefix(name, "TenGigabitEthernet"):
		return devicemodel.MediaSFPPlus
	case strings.HasPrefix(name, "GigabitEthernet"):
		return devicemodel.MediaSFP
	}
	return devicemodel.MediaUnknown
}

func speed(n node) int {
	s := n.text("ether-state", "negotiated-port-speed")
	values := map[string]int{"speed-10mb": 10, "speed-100mb": 100, "speed-1gb": 1000, "speed-10gb": 10000, "speed-40gb": 40000}
	if v := values[s]; v != 0 {
		return v
	}
	if n.text("oper-status") != "if-oper-state-ready" {
		return 0
	}
	// This capture reports 1024-based bandwidth (e.g. 1024000000 for 1G).
	// Normalize only the exact values observed on this release.
	v := n.uint("speed")
	for _, mbps := range []int{10, 100, 1000, 10000, 40000} {
		if v == uint64(mbps)*1024000 {
			return mbps
		}
	}
	return int(v / 1000000)
}

func configuredSpeed(config node) int {
	speed := config.child("speed")
	for _, mode := range speed.Children {
		if strings.HasPrefix(mode.XMLName.Local, "value-") {
			value, err := strconv.Atoi(strings.TrimPrefix(mode.XMLName.Local, "value-"))
			if err == nil {
				return value
			}
		}
	}
	return 0
}

func speedCaps(media devicemodel.Media) []int {
	switch media {
	case devicemodel.MediaCopper1G:
		return []int{10, 100, 1000}
	case devicemodel.MediaCopper10G, devicemodel.MediaSFPPlus, devicemodel.MediaSFP28:
		return []int{1000, 10000}
	case devicemodel.MediaSFP:
		return []int{1000}
	default:
		return nil
	}
}

func build(data map[string]node, now time.Time, module moduleOptions, switching bool) (*devicemodel.Snapshot, map[string]node, error) {
	native := nativePorts(data["native-interfaces"])
	snap := &devicemodel.Snapshot{TakenAt: now, System: devicemodel.System{Vendor: "Cisco", Hostname: data["identity"].text("native", "hostname"), ARP: map[string]string{}}}
	hw := data["Cisco-IOS-XE-device-hardware-oper"]
	for _, d := range hw.all("device-inventory") {
		if d.text("hw-type") == "hw-type-chassis" {
			snap.System.Model = d.text("part-number")
			snap.System.Serial = d.text("serial-number")
			break
		}
	}
	sys := hw.child("device-hardware-data").child("device-hardware").child("device-system-data")
	version := regexp.MustCompile(`Version ([^,\s]+)`).FindStringSubmatch(sys.text("software-version"))
	if len(version) > 1 {
		snap.System.Version = version[1]
	}
	current, e1 := time.Parse(time.RFC3339, sys.text("current-time"))
	boot, e2 := time.Parse(time.RFC3339, sys.text("boot-time"))
	if e1 == nil && e2 == nil && current.After(boot) {
		snap.System.Uptime = current.Sub(boot)
	}
	if snap.System.Model == "" || snap.System.Version == "" || snap.System.Hostname == "" {
		return nil, nil, fmt.Errorf("cisco-iosxe: incomplete identity")
	}
	platform := data["Cisco-IOS-XE-platform-oper"]
	present := map[string]bool{}
	for _, component := range platform.all("component") {
		state := component.child("state")
		typ := state.text("type")
		name := component.text("cname")
		empty := state.text("empty") == "true"
		ok := state.text("status") == "status-active"
		switch typ {
		case "comp-port":
			if !empty {
				present[name] = true
			}
		case "comp-sensor":
			temp := state.number("temp", "temp-instant")
			if temp > snap.System.TemperatureC {
				snap.System.TemperatureC = temp
				snap.System.HasTemperature = true
			}
		case "comp-fan":
			if !empty {
				snap.System.Fans = append(snap.System.Fans, devicemodel.Fan{Label: name, OK: ok})
			}
		case "comp-power-supply":
			snap.System.PSUs = append(snap.System.PSUs, devicemodel.PSU{Slot: name, Model: state.text("part-no"), Present: !empty, OK: !empty && ok})
		}
	}
	oper := data["Cisco-IOS-XE-interfaces-oper"]
	byName := map[string]node{}
	for _, n := range oper.all("interface") {
		name := n.text("name")
		byName[name] = n
		ip := n.text("ipv4")
		if addr := net.ParseIP(ip); addr != nil && addr.IsUnspecified() {
			ip = ""
		}
		mask := n.text("ipv4-subnet-mask")
		if ip != "" && ip != "0.0.0.0" {
			m := net.ParseIP(mask).To4()
			bits, _ := net.IPMask(m).Size()
			snap.System.Addresses = append(snap.System.Addresses, devicemodel.IfAddress{Iface: name, IP: ip, PrefixLen: bits})
			if strings.HasPrefix(name, "Vlan") {
				snap.System.MAC = normalizeMAC(n.text("phys-address"))
			}
		}
		if name == "GigabitEthernet0/0" {
			snap.System.MgmtMAC = normalizeMAC(n.text("phys-address"))
			snap.System.OOBInterfaces = append(snap.System.OOBInterfaces, devicemodel.OOBInterface{Name: name, Up: n.text("oper-status") == "if-oper-state-ready", IP: ip})
		}
	}
	var names []string
	modules, err := discoverModules(platform, hw, native, module)
	if err != nil {
		return nil, nil, err
	}
	for slot, model := range modules {
		for name := range present {
			order := portOrder(name)
			if len(order) == 3 && fmt.Sprintf("%d/%d", order[0], order[1]) == slot {
				delete(present, name)
			}
		}
		for _, name := range moduleInterfaces(model, slot, module.mode, byName) {
			if native[name].XMLName.Local == "" || byName[name].XMLName.Local == "" {
				return nil, nil, fmt.Errorf("cisco-iosxe: module interface %s is missing from NETCONF", name)
			}
			present[name] = true
		}
	}
	for name := range present {
		if module.model == "NONE" {
			order := portOrder(name)
			if len(order) == 3 && order[1] == 1 {
				continue
			}
		}
		if len(portOrder(name)) == 3 && native[name].XMLName.Local != "" && byName[name].XMLName.Local != "" {
			names = append(names, name)
		}
	}
	sort.Slice(names, func(i, j int) bool {
		a, b := portOrder(names[i]), portOrder(names[j])
		for k := range a {
			if a[k] != b[k] {
				return a[k] < b[k]
			}
		}
		return names[i] < names[j]
	})
	for i, name := range names {
		n, cfg := byName[name], native[name]
		vlan, err := vlanState(cfg, switching)
		if err != nil {
			return nil, nil, fmt.Errorf("%s VLAN: %w", name, err)
		}
		p := devicemodel.Port{Index: i + 1, IfName: name, Interfaces: []string{name}, Name: name, Description: cfg.text("description"), Present: true, Lanes: 1, Enabled: !cfg.has("shutdown"), Up: n.text("oper-status") == "if-oper-state-ready", SpeedMbps: speed(n), FullDuplex: n.text("ether-state", "negotiated-duplex-mode") == "full-duplex", MTU: int(n.uint("mtu")), VLAN: vlan, AdminSpeed: configuredSpeed(cfg), AutoNeg: configuredSpeed(cfg) == 0}
		if p.Description != "" {
			p.Name = p.Description
		}
		p.Media = portMedia(name)
		p.SpeedCaps = speedCaps(p.Media)
		stats := n.child("statistics")
		p.Counters = devicemodel.Counters{RxBytes: stats.uint("in-octets"), TxBytes: stats.uint("out-octets"), RxPackets: stats.uint("in-unicast-pkts") + stats.uint("in-multicast-pkts") + stats.uint("in-broadcast-pkts"), TxPackets: stats.uint("out-unicast-pkts") + stats.uint("out-multicast-pkts") + stats.uint("out-broadcast-pkts"), RxErrors: stats.uint("in-errors"), TxErrors: stats.uint("out-errors"), RxDropped: stats.uint("in-discards"), TxDropped: stats.uint("out-discards"), RxMulticast: stats.uint("in-multicast-pkts"), RxBroadcast: stats.uint("in-broadcast-pkts"), TxMulticast: stats.uint("out-multicast-pkts"), TxBroadcast: stats.uint("out-broadcast-pkts")}
		p.Health.LinkChanges = n.uint("num-flaps")
		if group := cfg.child("channel-group"); group.XMLName.Local != "" {
			p.LAGID = int(group.uint("number"))
			p.LAG = fmt.Sprintf("Port-channel%d", p.LAGID)
			if lag := native[p.LAG]; lag.XMLName.Local != "" {
				p.VLAN, err = vlanState(lag, switching)
				if err != nil {
					return nil, nil, err
				}
			}
		}
		p.BPDUGuard = cfg.child("spanning-tree").child("bpduguard").has("enable")
		stpConfig := cfg
		if p.LAG != "" {
			stpConfig = native[p.LAG]
		}
		p.STPEdge = stpEdgeState(stpConfig)
		snap.Ports = append(snap.Ports, p)
	}
	snap.Ports = foldModuleLanes(snap.Ports, modules)
	if len(snap.Ports) == 0 {
		return nil, nil, fmt.Errorf("cisco-iosxe: no physical ports matched platform inventory and interfaces")
	}
	local := data["Cisco-IOS-XE-lldp-oper"].child("lldp-entries").child("lldp-state-details")
	if mac := normalizeMAC(local.text("chassis-id")); mac != "" {
		snap.System.MAC = mac
	}
	if snap.System.MAC == "" {
		return nil, nil, fmt.Errorf("cisco-iosxe: no system MAC")
	}
	for _, v := range data["Cisco-IOS-XE-vlan-oper"].all("vlan") {
		id := int(v.uint("id"))
		if id > 0 {
			snap.VLANs = append(snap.VLANs, id)
		}
	}
	sort.Ints(snap.VLANs)
	for _, a := range data["Cisco-IOS-XE-arp-oper"].all("arp-oper") {
		if mac := normalizeMAC(a.text("hardware")); mac != "" {
			snap.System.ARP[a.text("address")] = mac
		}
	}
	for _, m := range data["Cisco-IOS-XE-memory-oper"].all("memory-statistic") {
		if m.text("name") == "Processor" {
			snap.System.MemTotalKB = m.uint("total-memory") / 1024
			snap.System.MemUsedKB = m.uint("used-memory") / 1024
		}
	}
	cpu := data["Cisco-IOS-XE-process-cpu-oper"].child("cpu-usage").child("cpu-utilization")
	snap.System.CPUPercent = cpu.number("five-seconds")
	snap.System.STPMode = data["spanning-tree"].text("native", "spanning-tree", "mode")
	addNeighbors(snap, data["Cisco-IOS-XE-lldp-oper"], data["Cisco-IOS-XE-cdp-oper"])
	return snap, native, nil
}

func addNeighbors(s *devicemodel.Snapshot, lldp, cdp node) {
	neighbors := map[string]*devicemodel.Neighbor{}
	for _, details := range lldp.all("lldp-intf-details") {
		entries := details.all("lldp-neighbor-details")
		if len(entries) == 0 {
			continue
		}
		e := entries[0]
		caps := e.child("enabled-capabilities")
		neighbors[details.text("if-name")] = &devicemodel.Neighbor{SystemName: e.text("system-name"), SystemDesc: e.text("system-desc"), ChassisID: normalizeMAC(e.text("chassis-id")), PortID: e.text("port-id"), PortDescription: e.text("port-desc"), ManagementIP: e.text("mgmt-addrs", "mgmt-addr"), IsBridge: caps.has("bridge"), IsRouter: caps.has("router")}
	}
	for _, e := range cdp.all("cdp-neighbor-detail") {
		name := canonicalInterface(e.text("local-intf-name"))
		if neighbors[name] != nil {
			continue
		}
		capability := e.text("capability")
		neighbors[name] = &devicemodel.Neighbor{SystemName: e.text("device-name"), SystemDesc: e.text("platform-name"), PortID: e.text("port-id"), IsBridge: strings.Contains(capability, "Switch"), IsRouter: strings.Contains(capability, "Router")}
	}
	for i := range s.Ports {
		s.Ports[i].Neighbor = neighbors[s.Ports[i].IfName]
		if s.Ports[i].Neighbor == nil {
			for _, iface := range s.Ports[i].Interfaces {
				if neighbors[iface] != nil {
					s.Ports[i].Neighbor = neighbors[iface]
					break
				}
			}
		}
	}
}
