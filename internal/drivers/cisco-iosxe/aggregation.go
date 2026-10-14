package ciscoiosxe

import (
	"slices"
	"sort"

	"github.com/TechBlueprints/disunified/internal/devicemodel"
)

func addAggregations(snap *devicemodel.Snapshot, data, interfaceData node) {
	oper := map[string]node{}
	for _, n := range data.all("interface") {
		oper[n.text("name")] = n
	}
	if len(oper) > 0 {
		snap.Aggregations = []devicemodel.Aggregation{}
	}
	physical := map[string]devicemodel.Port{}
	for _, p := range snap.Ports {
		physical[p.IfName] = p
		for _, iface := range p.Interfaces {
			physical[iface] = p
		}
	}
	for name, n := range oper {
		state := n.child("aggregation").child("state")
		if state.XMLName.Local == "" {
			continue
		}
		group := devicemodel.Aggregation{Name: name, Up: n.text("state", "oper-status") == "UP"}
		if stats, ok := interfaceDataFor(interfaceData, name); ok && stats.child("statistics").XMLName.Local != "" {
			group.Counters = countersFromInterface(stats)
			group.HasCounters = true
		}
		seen := map[int]bool{}
		for _, member := range state.Children {
			if member.XMLName.Local != "member" {
				continue
			}
			iface := canonicalInterface(member.Text)
			p, ok := physical[iface]
			if !ok || seen[p.Index] {
				continue
			}
			seen[p.Index] = true
			if group.ID == 0 {
				group.ID = p.LAGID
			}
			active := group.Up && oper[iface].text("state", "oper-status") == "UP"
			group.Members = append(group.Members, devicemodel.AggregationMember{PortIndex: p.Index, Active: active, SpeedMbps: p.SpeedMbps})
		}
		if len(group.Members) == 0 {
			continue
		}
		sort.Slice(group.Members, func(i, j int) bool { return group.Members[i].PortIndex < group.Members[j].PortIndex })
		snap.Aggregations = append(snap.Aggregations, group)
	}
	sort.Slice(snap.Aggregations, func(i, j int) bool { return snap.Aggregations[i].Name < snap.Aggregations[j].Name })
}

// switchLAGID translates UniFi's one-based logical LAG index to the Cisco
// channel-group ID. UniFi assigns logical indices by the groups' primary port
// order; Cisco IDs are independent and need not have the same values.
func switchLAGID(snap *devicemodel.Snapshot, logicalID int) int {
	if logicalID <= 0 {
		return 0
	}
	if snap == nil {
		return logicalID
	}

	groups := slices.Clone(snap.Aggregations)
	if len(groups) == 0 {
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
	}
	sort.Slice(groups, func(i, j int) bool {
		first := func(group devicemodel.Aggregation) int {
			if len(group.Members) == 0 {
				return int(^uint(0) >> 1)
			}
			lowest := group.Members[0].PortIndex
			for _, member := range group.Members[1:] {
				if member.PortIndex < lowest {
					lowest = member.PortIndex
				}
			}
			return lowest
		}
		left, right := first(groups[i]), first(groups[j])
		if left != right {
			return left < right
		}
		return groups[i].ID < groups[j].ID
	})
	used := make(map[int]bool, len(groups))
	for _, group := range groups {
		used[group.ID] = true
	}
	if logicalID <= len(groups) {
		return groups[logicalID-1].ID
	}
	if !used[logicalID] && logicalID <= 512 {
		return logicalID
	}
	for id := 1; id <= 512; id++ {
		if !used[id] {
			return id
		}
	}
	return -1
}

func interfaceDataFor(data node, name string) (node, bool) {
	want := canonicalInterface(name)
	for _, iface := range data.all("interface") {
		if canonicalInterface(iface.text("name")) == want {
			return iface, true
		}
	}
	return node{}, false
}

func countersFromInterface(iface node) devicemodel.Counters {
	stats := iface.child("statistics")
	return devicemodel.Counters{
		RxBytes:     stats.uint("in-octets"),
		TxBytes:     stats.uint("out-octets"),
		RxPackets:   stats.uint("in-unicast-pkts") + stats.uint("in-multicast-pkts") + stats.uint("in-broadcast-pkts"),
		TxPackets:   stats.uint("out-unicast-pkts") + stats.uint("out-multicast-pkts") + stats.uint("out-broadcast-pkts"),
		RxErrors:    stats.uint("in-errors"),
		TxErrors:    stats.uint("out-errors"),
		RxDropped:   stats.uint("in-discards"),
		TxDropped:   stats.uint("out-discards"),
		RxMulticast: stats.uint("in-multicast-pkts"),
		RxBroadcast: stats.uint("in-broadcast-pkts"),
		TxMulticast: stats.uint("out-multicast-pkts"),
		TxBroadcast: stats.uint("out-broadcast-pkts"),
	}
}
