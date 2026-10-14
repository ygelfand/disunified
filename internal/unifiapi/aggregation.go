package unifiapi

import (
	"fmt"
	"sort"

	"github.com/TechBlueprints/disunified/internal/devicemodel"
)

// seedAggregations creates only groups whose members have no conflicting
// controller configuration. Membership and forwarding come from the switch.
func seedAggregations(snap *devicemodel.Snapshot, nets []Network, overrides map[int]map[string]any) (int, []string) {
	ports := make(map[int]devicemodel.Port, len(snap.Ports))
	for _, port := range snap.Ports {
		ports[port.Index] = port
	}
	byVLAN := make(map[int]string, len(nets))
	for _, network := range nets {
		if network.Enabled {
			byVLAN[network.VLAN] = network.ID
		}
	}
	count := 0
	var notes []string
	groups := snap.Aggregations
	if groups == nil {
		byID := map[int]*devicemodel.Aggregation{}
		for _, port := range snap.Ports {
			if port.LAGID <= 0 {
				continue
			}
			group := byID[port.LAGID]
			if group == nil {
				group = &devicemodel.Aggregation{ID: port.LAGID, Name: port.LAG}
				byID[port.LAGID] = group
			}
			group.Members = append(group.Members, devicemodel.AggregationMember{PortIndex: port.Index})
		}
		for _, group := range byID {
			groups = append(groups, *group)
		}
		sort.Slice(groups, func(i, j int) bool { return groups[i].ID < groups[j].ID })
	}
	for _, group := range groups {
		members := make([]int, 0, len(group.Members))
		valid := group.ID > 0 && len(group.Members) > 1
		seen := map[int]bool{}
		for _, member := range group.Members {
			port, ok := ports[member.PortIndex]
			valid = valid && ok && port.Present && !seen[member.PortIndex]
			seen[member.PortIndex] = true
			members = append(members, member.PortIndex)
		}
		if !valid {
			notes = append(notes, fmt.Sprintf("aggregation %s: invalid or absent members; left unchanged", group.Name))
			continue
		}
		sort.Ints(members)
		primary := members[0]
		current := overrides[primary]
		if sameJSON(current["aggregate_members"], members) && current["op_mode"] == "aggregate" {
			continue
		}
		port := ports[primary]
		// Link/admin state belongs to each member; a down primary must not
		// disable the aggregate's other members in controller configuration.
		port.Enabled = true
		forwarding, note := overrideFor(port, byVLAN, byVLAN[1])
		if note != "" {
			notes = append(notes, fmt.Sprintf("aggregation %s: %s", group.Name, note))
			continue
		}
		conflict := false
		for _, idx := range members {
			o := overrides[idx]
			mode, _ := o["op_mode"].(string)
			if mode != "" && mode != "switch" || o["aggregate_members"] != nil || o["lag_idx"] != nil {
				conflict = true
			}
			if configured(o) {
				if o["portconf_id"] != nil || o["port_security_enabled"] == true {
					conflict = true
				}
				for key, value := range forwarding {
					if !sameJSON(o[key], value) {
						conflict = true
					}
				}
				if forwarding == nil {
					conflict = true
				}
			}
		}
		// A stored group elsewhere may already own one of these ports.
		for idx, o := range overrides {
			if seen[idx] {
				continue
			}
			for _, member := range members {
				if containsPort(o["aggregate_members"], member) {
					conflict = true
				}
			}
		}
		if conflict {
			notes = append(notes, fmt.Sprintf("aggregation %s: controller configuration conflicts; left unchanged", group.Name))
			continue
		}
		if current == nil {
			current = map[string]any{"port_idx": primary}
			overrides[primary] = current
		}
		for key, value := range forwarding {
			current[key] = value
		}
		current["op_mode"] = "aggregate"
		current["aggregate_members"] = members
		count++
	}
	return count, notes
}

func containsPort(value any, idx int) bool {
	switch members := value.(type) {
	case []any:
		for _, member := range members {
			if sameJSON(member, idx) {
				return true
			}
		}
	case []int:
		for _, member := range members {
			if member == idx {
				return true
			}
		}
	}
	return false
}
