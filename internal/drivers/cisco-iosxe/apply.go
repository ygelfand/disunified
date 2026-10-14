package ciscoiosxe

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/TechBlueprints/disunified/internal/devicemodel"
)

const operationNS = `xmlns:nc="urn:ietf:params:xml:ns:netconf:base:1.0"`

func escape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func desiredVLAN(d devicemodel.PortDesired) devicemodel.PortVLAN {
	v := devicemodel.PortVLAN{Mode: "access", NativeVLAN: d.NativeVLAN}
	if d.TaggedAll || len(d.TaggedVLANs) > 0 {
		v.Mode = "trunk"
		v.AllowAll = d.TaggedAll
		v.Allowed = slices.Clone(d.TaggedVLANs)
		sortVLANs(v.Allowed)
		if !v.AllowAll && !slices.Contains(v.Allowed, d.NativeVLAN) {
			v.Allowed = append(v.Allowed, d.NativeVLAN)
			sortVLANs(v.Allowed)
		}
	}
	return v
}

func sortVLANs(ids []int) { slices.Sort(ids) }

// EnsureVLANs creates site VLANs that are missing from the switch and verifies
// them through the operational VLAN table before reporting convergence.
func (c *Collector) EnsureVLANs(ctx context.Context, ids []int) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	snap := c.last
	if snap == nil {
		return 0, fmt.Errorf("cisco-iosxe: no snapshot")
	}
	have := make(map[int]bool, len(snap.VLANs))
	for _, id := range snap.VLANs {
		have[id] = true
	}

	missing := make([]int, 0, len(ids))
	seen := map[int]bool{}
	for _, id := range ids {
		if id < 1 || id > 4094 {
			return 0, fmt.Errorf("cisco-iosxe: invalid VLAN %d", id)
		}
		if !have[id] && !seen[id] {
			missing = append(missing, id)
			seen[id] = true
		}
	}
	if len(missing) == 0 {
		return 0, nil
	}
	sortVLANs(missing)

	var config strings.Builder
	config.WriteString(`<native xmlns="` + nativeNS + `"><vlan xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-vlan">`)
	for _, id := range missing {
		fmt.Fprintf(&config, "<vlan-list><id>%d</id></vlan-list>", id)
	}
	config.WriteString(`</vlan></native>`)
	if err := c.t.Edit(ctx, config.String()); err != nil {
		return 0, fmt.Errorf("cisco-iosxe create VLANs %v: %w", missing, err)
	}
	if err := c.markStartupPending(); err != nil {
		return 0, fmt.Errorf("record pending startup save: %w", err)
	}

	b, err := c.t.Get(ctx, `<vlans xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-vlan-oper"/>`)
	if err != nil {
		return 0, fmt.Errorf("cisco-iosxe VLAN readback: %w", err)
	}
	state, err := decode(b)
	if err != nil {
		return 0, fmt.Errorf("cisco-iosxe VLAN readback XML: %w", err)
	}
	confirmed := map[int]bool{}
	for _, vlan := range state.all("vlan") {
		confirmed[int(vlan.uint("id"))] = true
	}
	for _, id := range missing {
		if !confirmed[id] {
			return 0, fmt.Errorf("cisco-iosxe VLAN readback: VLAN %d is still missing", id)
		}
	}

	if c.last == snap {
		updated := *snap
		updated.VLANs = append(slices.Clone(snap.VLANs), missing...)
		sortVLANs(updated.VLANs)
		c.last = &updated
	}
	return len(missing), c.saveStartup(ctx)
}

func sameVLAN(a, b devicemodel.PortVLAN) bool {
	return a.Mode == b.Mode && a.NativeVLAN == b.NativeVLAN && a.AllowAll == b.AllowAll && (a.AllowAll || slices.Equal(a.Allowed, b.Allowed))
}

func portDiff(p devicemodel.Port, d devicemodel.PortDesired) bool {
	d = preserveUnknownVLAN(p, d)
	return p.Description != d.Description || p.Enabled != d.Enabled || p.AdminSpeed != d.SpeedMbps || p.BPDUGuard != d.BPDUGuard || p.STPEdge != d.STPEdge || p.LAGID != d.LAG || (d.VLANSet && !sameVLAN(p.VLAN, desiredVLAN(d))) || (d.PoEMode != "" && p.PoE != nil && p.PoE.Mode != d.PoEMode)
}

func preserveUnknownVLAN(p devicemodel.Port, d devicemodel.PortDesired) devicemodel.PortDesired {
	// An omitted switchport stanza does not distinguish routed ports
	// from switchport defaults. A controller's default VLAN bundle must
	// not block description/admin changes or convert the port's mode.
	if p.VLAN.Mode == "" && d.VLANSet && d.NativeVLAN >= 0 && d.NativeVLAN <= 1 && len(d.TaggedVLANs) == 0 {
		d.VLANSet = false
	}
	return d
}

// PlanPorts shares the exact diff used by ApplyPorts and never writes.
func (c *Collector) PlanPorts(desired []devicemodel.PortDesired) []int {
	c.mu.Lock()
	defer c.mu.Unlock()
	var changed []int
	if c.last == nil {
		return changed
	}
	for _, d := range desired {
		for _, p := range c.last.Ports {
			if p.Index != d.Index {
				continue
			}
			d = preserveUnknownVLAN(p, d)
			d.LAG = switchLAGID(c.last, d.LAG)
			cleanup := d.LAG == 0 && accessTrunkCleanup(c.native[p.IfName], d)
			if portDiff(p, d) || c.lagVLANDiff(d) || cleanup {
				changed = append(changed, p.Index)
			}
			break
		}
	}
	return changed
}

func (c *Collector) lagVLANDiff(d devicemodel.PortDesired) bool {
	if !d.VLANSet || d.LAG <= 0 {
		return false
	}
	config, ok := c.native[fmt.Sprintf("Port-channel%d", d.LAG)]
	if !ok {
		return true
	}
	current, err := vlanState(config, c.switching)
	return err != nil || !sameVLAN(current, desiredVLAN(d))
}

// ApplyPorts supports descriptions, admin state, speed, spanning-tree controls,
// PoE, LACP membership, and VLAN membership.
// It validates every target before sending one rollback-on-error edit and
// verifies the resulting native configuration before updating its diff state.
func (c *Collector) ApplyPorts(ctx context.Context, desired []devicemodel.PortDesired) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.last == nil {
		return 0, fmt.Errorf("cisco-iosxe: no snapshot")
	}
	ports := map[int]devicemodel.Port{}
	for _, p := range c.last.Ports {
		ports[p.Index] = p
	}
	var changed []devicemodel.PortDesired
	var edits strings.Builder
	groupEdits := map[int]string{}
	seen := map[int]bool{}
	for _, d := range desired {
		p, ok := ports[d.Index]
		if !ok {
			// The controller model can include profile-only positions that the
			// device's hardware inventory does not report. There is no interface
			// to configure at those indexes.
			continue
		}
		if seen[d.Index] {
			return 0, fmt.Errorf("cisco-iosxe: duplicate port %d", d.Index)
		}
		seen[d.Index] = true
		d = preserveUnknownVLAN(p, d)
		d.LAG = switchLAGID(c.last, d.LAG)
		if d.PoEMode != "" {
			if d.PoEMode != "auto" && d.PoEMode != "off" {
				return 0, fmt.Errorf("cisco-iosxe: unsupported PoE mode %q", d.PoEMode)
			}
			if p.PoE == nil {
				return 0, fmt.Errorf("cisco-iosxe: PoE is not supported on %s", p.IfName)
			}
			if p.PoE.Mode == "static" && d.PoEMode != p.PoE.Mode {
				return 0, fmt.Errorf("cisco-iosxe: changing static PoE mode is not supported on %s", p.IfName)
			}
		}
		if d.SpeedMbps < 0 || (d.SpeedMbps > 0 && !slices.Contains(p.SpeedCaps, d.SpeedMbps)) {
			return 0, fmt.Errorf("cisco-iosxe: unsupported speed %d Mbps for %s", d.SpeedMbps, p.IfName)
		}
		if d.LAG < 0 || d.LAG > 512 {
			return 0, fmt.Errorf("cisco-iosxe: invalid LAG id %d", d.LAG)
		}
		if p.STPEdge != d.STPEdge && p.LAGID > 0 {
			return 0, fmt.Errorf("cisco-iosxe: STP edge mode changes on port-channel members are not supported")
		}
		if d.LAG > 0 && p.LAGID != d.LAG && (p.Lanes > 1 || p.VLAN.Mode == "") {
			return 0, fmt.Errorf("cisco-iosxe: cannot add routed or breakout interface %s to a port-channel", p.IfName)
		}
		if d.MirrorSource != 0 || d.FEC != nil || d.StormCtrl != nil {
			return 0, fmt.Errorf("cisco-iosxe: mirroring, FEC and storm control writes are not implemented")
		}
		cleanupTrunk := d.LAG == 0 && accessTrunkCleanup(c.native[p.IfName], d)
		if !portDiff(p, d) && !c.lagVLANDiff(d) && !cleanupTrunk {
			continue
		}
		if !p.Present {
			return 0, fmt.Errorf("cisco-iosxe: interface %s is not present", p.IfName)
		}
		if p.Lanes > 1 {
			return 0, fmt.Errorf("cisco-iosxe: configuration of breakout cages is not supported")
		}
		if d.VLANSet && (!sameVLAN(p.VLAN, desiredVLAN(d)) || cleanupTrunk) {
			if p.VLAN.Mode == "" {
				return 0, fmt.Errorf("cisco-iosxe: refusing VLAN changes on routed interface %s", p.IfName)
			}
			ids := append(slices.Clone(d.TaggedVLANs), d.NativeVLAN)
			for _, id := range ids {
				if id < 1 || id > 4094 || !slices.Contains(c.last.VLANs, id) {
					return 0, fmt.Errorf("cisco-iosxe: VLAN %d is invalid or does not exist", id)
				}
			}
		}
		family := c.native[p.IfName].XMLName.Local
		name := strings.TrimPrefix(p.IfName, family)
		if family == "" {
			return 0, fmt.Errorf("cisco-iosxe: native interface missing for %s", p.IfName)
		}
		var portEdit strings.Builder
		portEdit.WriteString("<" + family + "><name>" + escape(name) + "</name>")
		contentStart := portEdit.Len()
		if p.Description != d.Description {
			if d.Description == "" {
				portEdit.WriteString(`<description ` + operationNS + ` nc:operation="remove"/>`)
			} else {
				portEdit.WriteString("<description>" + escape(d.Description) + "</description>")
			}
		}
		if p.Enabled != d.Enabled {
			if d.Enabled {
				portEdit.WriteString(`<shutdown ` + operationNS + ` nc:operation="remove"/>`)
			} else {
				portEdit.WriteString("<shutdown/>")
			}
		}
		if p.AdminSpeed != d.SpeedMbps {
			portEdit.WriteString(speedEdit(d.SpeedMbps))
		}
		if d.PoEMode != "" && p.PoE != nil && p.PoE.Mode != d.PoEMode {
			portEdit.WriteString(poeEdit(d.PoEMode, c.native[p.IfName]))
		}
		if p.BPDUGuard != d.BPDUGuard {
			portEdit.WriteString(bpduGuardEdit(d.BPDUGuard))
		}
		if p.STPEdge != d.STPEdge {
			portEdit.WriteString(stpEdgeEdit(d.STPEdge))
		}
		if p.LAGID != d.LAG {
			portEdit.WriteString(lagEdit(d.LAG))
		}
		if d.VLANSet && d.LAG == 0 && (!sameVLAN(p.VLAN, desiredVLAN(d)) || p.LAGID > 0 || cleanupTrunk) {
			portEdit.WriteString(vlanEdit(desiredVLAN(d)))
		}
		if portEdit.Len() > contentStart {
			portEdit.WriteString("</" + family + ">")
			edits.WriteString(portEdit.String())
		}
		if d.VLANSet && d.LAG > 0 && c.lagVLANDiff(d) {
			groupVLAN := vlanEdit(desiredVLAN(d))
			if previous, ok := groupEdits[d.LAG]; ok && previous != groupVLAN {
				return 0, fmt.Errorf("cisco-iosxe: conflicting VLAN settings for port-channel %d", d.LAG)
			}
			groupEdits[d.LAG] = groupVLAN
		}
		changed = append(changed, d)
	}
	groupIDs := make([]int, 0, len(groupEdits))
	for id := range groupEdits {
		groupIDs = append(groupIDs, id)
	}
	slices.Sort(groupIDs)
	for _, id := range groupIDs {
		fmt.Fprintf(&edits, `<Port-channel><name>%d</name>%s</Port-channel>`, id, groupEdits[id])
	}
	if len(changed) == 0 {
		return 0, c.saveStartup(ctx)
	}
	config := `<native xmlns="` + nativeNS + `"><interface>` + edits.String() + `</interface></native>`
	if err := c.t.Edit(ctx, config); err != nil {
		return 0, fmt.Errorf("cisco-iosxe edit: %w", err)
	}
	if err := c.markStartupPending(); err != nil {
		return 0, fmt.Errorf("record pending startup save: %w", err)
	}
	b, err := c.t.GetConfig(ctx, `<native xmlns="`+nativeNS+`"><interface/></native>`)
	if err != nil {
		return 0, fmt.Errorf("cisco-iosxe readback: %w", err)
	}
	n, err := decode(b)
	if err != nil {
		return 0, err
	}
	current := nativePorts(n)
	updated := *c.last
	updated.Ports = slices.Clone(c.last.Ports)
	for _, d := range changed {
		p := ports[d.Index]
		cfg, ok := current[p.IfName]
		if !ok {
			return 0, fmt.Errorf("cisco-iosxe: readback missing %s", p.IfName)
		}
		if cfg.text("description") != d.Description || !cfg.has("shutdown") != d.Enabled {
			return 0, fmt.Errorf("cisco-iosxe: readback differs for %s", p.IfName)
		}
		if configuredSpeed(cfg) != d.SpeedMbps {
			return 0, fmt.Errorf("cisco-iosxe: speed readback differs for %s", p.IfName)
		}
		v, err := vlanState(cfg, c.switching)
		if err != nil {
			return 0, err
		}
		if d.VLANSet {
			vlanConfig := cfg
			if d.LAG > 0 {
				var ok bool
				vlanConfig, ok = current[fmt.Sprintf("Port-channel%d", d.LAG)]
				if !ok {
					return 0, fmt.Errorf("cisco-iosxe: VLAN readback missing port-channel %d", d.LAG)
				}
			}
			v, err = vlanState(vlanConfig, c.switching)
			if err != nil {
				return 0, err
			}
			if !sameVLAN(v, desiredVLAN(d)) {
				return 0, fmt.Errorf("cisco-iosxe: VLAN readback differs for %s", p.IfName)
			}
		}
		if d.LAG != p.LAGID && int(cfg.uint("channel-group", "number")) != d.LAG {
			return 0, fmt.Errorf("cisco-iosxe: LAG readback differs for %s", p.IfName)
		}
		if d.LAG == 0 && accessTrunkCleanup(cfg, d) {
			return 0, fmt.Errorf("cisco-iosxe: trunk settings remain on access port %s", p.IfName)
		}
		if d.PoEMode != "" && nativePoEMode(cfg) != d.PoEMode {
			return 0, fmt.Errorf("cisco-iosxe: PoE readback differs for %s", p.IfName)
		}
		if cfg.child("spanning-tree").child("bpduguard").has("enable") != d.BPDUGuard {
			return 0, fmt.Errorf("cisco-iosxe: BPDU guard readback differs for %s", p.IfName)
		}
		if stpEdgeState(cfg) != d.STPEdge {
			return 0, fmt.Errorf("cisco-iosxe: STP edge readback differs for %s", p.IfName)
		}
		for i := range updated.Ports {
			if updated.Ports[i].Index == d.Index {
				updated.Ports[i].Description = d.Description
				updated.Ports[i].Enabled = d.Enabled
				updated.Ports[i].AdminSpeed = d.SpeedMbps
				updated.Ports[i].AutoNeg = d.SpeedMbps == 0
				updated.Ports[i].BPDUGuard = d.BPDUGuard
				updated.Ports[i].STPEdge = d.STPEdge
				updated.Ports[i].LAGID = d.LAG
				updated.Ports[i].LAG = ""
				if d.LAG > 0 {
					updated.Ports[i].LAG = fmt.Sprintf("Port-channel%d", d.LAG)
				}
				if d.PoEMode != "" && p.PoE != nil {
					power := *p.PoE
					power.Mode = d.PoEMode
					updated.Ports[i].PoE = &power
				}
				if d.VLANSet {
					updated.Ports[i].VLAN = v
				}
			}
		}
	}
	c.last = &updated
	c.native = current
	if c.saveConfig {
		if err := c.saveStartup(ctx); err != nil {
			return 0, err
		}
	}
	return len(changed), nil
}

func bpduGuardEdit(enabled bool) string {
	value := "<disable/>"
	if enabled {
		value = "<enable/>"
	}
	return `<spanning-tree xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-spanning-tree"><bpduguard xmlns:nc="urn:ietf:params:xml:ns:netconf:base:1.0" nc:operation="replace">` + value + `</bpduguard></spanning-tree>`
}

func speedEdit(mbps int) string {
	const namespace = `http://cisco.com/ns/yang/Cisco-IOS-XE-ethernet`
	if mbps == 0 {
		return `<speed xmlns="` + namespace + `" xmlns:nc="urn:ietf:params:xml:ns:netconf:base:1.0" nc:operation="replace"><auto/></speed>`
	}
	return fmt.Sprintf(`<speed xmlns="%s" xmlns:nc="urn:ietf:params:xml:ns:netconf:base:1.0" nc:operation="replace"><value-%d/></speed>`, namespace, mbps)
}

func stpEdgeState(config node) bool {
	stp := config.child("spanning-tree")
	return stp.child("portfast").has("edge") && stp.text("bpdufilter") == "enable"
}

func stpEdgeEdit(enabled bool) string {
	portfast, bpdufilter := "disable", "disable"
	if enabled {
		portfast, bpdufilter = "edge", "enable"
	}
	const namespace = `http://cisco.com/ns/yang/Cisco-IOS-XE-spanning-tree`
	return `<spanning-tree xmlns="` + namespace + `"><portfast xmlns:nc="urn:ietf:params:xml:ns:netconf:base:1.0" nc:operation="replace"><` + portfast + `/></portfast><bpdufilter xmlns:nc="urn:ietf:params:xml:ns:netconf:base:1.0" nc:operation="replace">` + bpdufilter + `</bpdufilter></spanning-tree>`
}

func lagEdit(id int) string {
	const ns = `http://cisco.com/ns/yang/Cisco-IOS-XE-ethernet`
	if id == 0 {
		return `<channel-group xmlns="` + ns + `" xmlns:nc="urn:ietf:params:xml:ns:netconf:base:1.0" nc:operation="remove"/>`
	}
	return fmt.Sprintf(`<channel-group xmlns="%s"><number>%d</number><mode>active</mode></channel-group>`, ns, id)
}

func vlanEdit(v devicemodel.PortVLAN) string {
	// Replace only mode and the allowed-VLAN leaf. Merge the native/access
	// VLAN, preserving encapsulation, pruning, voice VLANs and other settings.
	start := `<switchport><mode xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-switch" ` + operationNS + ` nc:operation="replace"><` + v.Mode + `/></mode>`
	if v.Mode == "access" {
		return start + `<access xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-switch"><vlan><vlan>` + strconv.Itoa(v.NativeVLAN) + `</vlan></vlan></access><trunk xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-switch"><native><vlan ` + operationNS + ` nc:operation="remove"/></native><allowed><vlan ` + operationNS + ` nc:operation="remove"/></allowed></trunk></switchport>`
	}
	allowed := "all"
	if !v.AllowAll {
		var ids []string
		for _, id := range v.Allowed {
			ids = append(ids, strconv.Itoa(id))
		}
		allowed = strings.Join(ids, ",")
	}
	return start + `<trunk xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-switch"><native><vlan>` + strconv.Itoa(v.NativeVLAN) + `</vlan></native><allowed><vlan><vlans ` + operationNS + ` nc:operation="replace">` + allowed + `</vlans></vlan></allowed></trunk></switchport>`
}

func accessTrunkCleanup(cfg node, desired devicemodel.PortDesired) bool {
	if !desired.VLANSet || desiredVLAN(desired).Mode != "access" {
		return false
	}
	trunk := cfg.child("switchport").child("trunk")
	return trunk.child("native").has("vlan") || trunk.child("allowed").has("vlan")
}
