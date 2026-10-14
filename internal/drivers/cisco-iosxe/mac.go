package ciscoiosxe

import (
	"sort"
	"strconv"

	"github.com/TechBlueprints/disunified/internal/devicemodel"
)

func addLearnedMACs(snapshot *devicemodel.Snapshot, data node) {
	byName := map[string]int{}
	for i, port := range snapshot.Ports {
		byName[port.IfName] = i
	}
	for _, group := range snapshot.Aggregations {
		if len(group.Members) > 0 {
			for i, port := range snapshot.Ports {
				if port.Index == group.Members[0].PortIndex {
					byName[group.Name] = i
					break
				}
			}
		}
	}
	seen := map[string]bool{}
	for _, row := range data.all("matm-mac-entry") {
		if row.text("mat-addr-type") != "dynamic" || row.text("port") == "CPU" {
			continue
		}
		mac := normalizeMAC(row.text("mac"))
		if mac == "" || mac == "00:00:00:00:00:00" || mac == snapshot.System.MAC {
			continue
		}
		first, _ := strconv.ParseUint(mac[:2], 16, 8)
		if first&1 != 0 {
			continue
		}
		vlan := int(row.uint("vlan-id-number"))
		key := mac + "/" + strconv.Itoa(vlan)
		if seen[key] {
			continue
		}
		seen[key] = true
		entry := devicemodel.MACEntry{MAC: mac, VLAN: vlan}
		if index, ok := byName[canonicalInterface(row.text("port"))]; ok {
			entry.PortIndex = snapshot.Ports[index].Index
			snapshot.Ports[index].MACs = append(snapshot.Ports[index].MACs, entry)
		}
		snapshot.MACTable = append(snapshot.MACTable, entry)
	}
	sort.Slice(snapshot.MACTable, func(i, j int) bool {
		a, b := snapshot.MACTable[i], snapshot.MACTable[j]
		if a.MAC != b.MAC {
			return a.MAC < b.MAC
		}
		return a.VLAN < b.VLAN
	})
	snapshot.System.MACTableUsed = len(snapshot.MACTable)
}
