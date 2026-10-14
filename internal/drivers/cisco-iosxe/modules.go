package ciscoiosxe

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/TechBlueprints/disunified/internal/devicemodel"
)

type moduleOptions struct{ model, mode string }

func parseModuleOptions(options map[string]string) (moduleOptions, error) {
	o := moduleOptions{model: strings.ToUpper(strings.TrimSpace(options["uplink_module"])), mode: strings.ToLower(strings.TrimSpace(options["uplink_mode"]))}
	if o.model == "AUTO" {
		o.model = ""
	}
	if o.mode == "" {
		o.mode = "auto"
	}
	if o.mode != "auto" && o.mode != "native" && o.mode != "breakout" {
		return o, fmt.Errorf("cisco-iosxe: uplink_mode must be auto, native or breakout")
	}
	if o.model != "" && o.model != "NONE" && !knownModule(o.model) {
		return o, fmt.Errorf("cisco-iosxe: unsupported uplink_module %q", o.model)
	}
	if o.mode == "breakout" && o.model != "" && o.model != "C3850-NM-2-40G" {
		return o, fmt.Errorf("cisco-iosxe: breakout mode requires a QSFP module")
	}
	return o, nil
}

func knownModule(model string) bool {
	switch model {
	case "C3850-NM-4-1G", "C3850-NM-2-10G", "C3850-NM-4-10G", "C3850-NM-8-10G", "C3850-NM-2-40G":
		return true
	}
	return false
}

// Module interface families follow Cisco's hardware installation guide.
// The module identifier is discovered or explicitly supplied by the operator.
func moduleInterfaces(model, slot, mode string, oper map[string]node) []string {
	var out []string
	add := func(family string, first, last int) {
		for p := first; p <= last; p++ {
			out = append(out, family+slot+"/"+strconv.Itoa(p))
		}
	}
	switch model {
	case "C3850-NM-4-1G":
		add("GigabitEthernet", 1, 4)
	case "C3850-NM-2-10G":
		add("GigabitEthernet", 1, 2)
		add("TenGigabitEthernet", 3, 4)
	case "C3850-NM-4-10G":
		add("TenGigabitEthernet", 1, 4)
	case "C3850-NM-8-10G":
		add("TenGigabitEthernet", 1, 8)
	case "C3850-NM-2-40G":
		for cage := 1; cage <= 2; cage++ {
			breakout := mode == "breakout"
			if mode == "auto" {
				for lane := (cage-1)*4 + 1; lane <= cage*4; lane++ {
					n := oper["TenGigabitEthernet"+slot+"/"+strconv.Itoa(lane)]
					status := n.text("oper-status")
					if status != "" && status != "if-oper-state-not-present" {
						breakout = true
					}
				}
			}
			if breakout {
				add("TenGigabitEthernet", (cage-1)*4+1, cage*4)
			} else {
				add("FortyGigabitEthernet", cage, cage)
			}
		}
	}
	return out
}

func discoverModules(platform, hw node, native map[string]node, options moduleOptions) (map[string]string, error) {
	out := map[string]string{}
	members := map[int]bool{}
	for name := range native {
		order := portOrder(name)
		if len(order) == 3 && order[1] == 0 {
			members[order[0]] = true
		}
	}
	if options.model == "NONE" {
		return out, nil
	}
	if options.model != "" {
		for member := range members {
			out[fmt.Sprintf("%d/1", member)] = options.model
		}
		return out, nil
	}
	for _, c := range platform.all("component") {
		model := c.text("state", "part-no")
		position := portOrder(c.text("cname"))
		if knownModule(model) && len(position) == 2 {
			out[fmt.Sprintf("%d/%d", position[0], position[1])] = model
		}
	}
	memberPattern := regexp.MustCompile(`(?i)(?:switch|member)\s*(\d+)`)
	for _, d := range hw.all("device-inventory") {
		model := d.text("part-number")
		if !knownModule(model) {
			continue
		}
		name := d.text("dev-name") + " " + d.text("hw-description")
		match := memberPattern.FindStringSubmatch(name)
		if len(match) > 1 {
			out[match[1]+"/1"] = model
			continue
		}
		if len(members) == 1 {
			for member := range members {
				out[fmt.Sprintf("%d/1", member)] = model
			}
			continue
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("cisco-iosxe: module inventory does not identify its stack member; configure uplink_module")
		}
	}
	return out, nil
}

func foldModuleLanes(ports []devicemodel.Port, modules map[string]string) []devicemodel.Port {
	var out []devicemodel.Port
	groups := map[string][]devicemodel.Port{}
	for _, p := range ports {
		order := portOrder(p.IfName)
		if len(order) == 3 && modules[fmt.Sprintf("%d/%d", order[0], order[1])] == "C3850-NM-2-40G" && strings.HasPrefix(p.IfName, "TenGigabitEthernet") {
			cage := (order[2]-1)/4 + 1
			key := fmt.Sprintf("FortyGigabitEthernet%d/%d/%d", order[0], order[1], cage)
			groups[key] = append(groups[key], p)
		} else {
			out = append(out, p)
		}
	}
	for name, lanes := range groups {
		sort.Slice(lanes, func(i, j int) bool { return portOrder(lanes[i].IfName)[2] < portOrder(lanes[j].IfName)[2] })
		p := lanes[0]
		p.IfName = name
		p.Name = name
		p.Media = devicemodel.MediaQSFPPlus
		p.Lanes = len(lanes)
		p.Interfaces = nil
		p.Counters = devicemodel.Counters{}
		p.Up = false
		p.Enabled = true
		p.Health.LinkChanges = 0
		for _, lane := range lanes {
			p.Interfaces = append(p.Interfaces, lane.IfName)
			p.Counters.Add(lane.Counters)
			p.Up = p.Up || lane.Up
			p.Enabled = p.Enabled && lane.Enabled
			p.Health.LinkChanges += lane.Health.LinkChanges
			if lane.Up && p.SpeedMbps == 0 {
				p.SpeedMbps = lane.SpeedMbps
			}
			if lane.Description != lanes[0].Description || lane.Enabled != lanes[0].Enabled || !sameVLAN(lane.VLAN, lanes[0].VLAN) {
				p.LanesDiverge = true
			}
		}
		if p.Description != "" {
			p.Name = p.Description
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := portOrder(out[i].IfName), portOrder(out[j].IfName)
		for k := range a {
			if a[k] != b[k] {
				return a[k] < b[k]
			}
		}
		return out[i].IfName < out[j].IfName
	})
	for i := range out {
		out[i].Index = i + 1
	}
	return out
}
