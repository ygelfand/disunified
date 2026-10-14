package ciscoiosxe

import (
	"fmt"
	"strings"

	"github.com/TechBlueprints/disunified/internal/devicemodel"
)

// An aggregate is represented by one physical member in the shared uplink
// field. All member ports retain their individual counters and LAG metadata.
func configuredUplink(s *devicemodel.Snapshot, interfaces string) (int, error) {
	var preferred int
	for _, entry := range strings.Split(interfaces, ",") {
		iface := canonicalInterface(strings.TrimSpace(entry))
		if iface == "" {
			return 0, fmt.Errorf("cisco-iosxe: uplink contains an empty interface")
		}
		idx, up, err := uplinkCandidate(s, iface)
		if err != nil {
			return 0, err
		}
		if preferred == 0 || (up && !portIsUp(s, preferred)) {
			preferred = idx
		}
	}
	return preferred, nil
}

func portIsUp(s *devicemodel.Snapshot, index int) bool {
	for _, p := range s.Ports {
		if p.Index == index {
			return p.Up
		}
	}
	return false
}

func uplinkCandidate(s *devicemodel.Snapshot, iface string) (int, bool, error) {
	best := 0
	for _, p := range s.Ports {
		if strings.EqualFold(p.IfName, iface) {
			return p.Index, p.Up, nil
		}
		for _, lane := range p.Interfaces {
			if strings.EqualFold(lane, iface) {
				return p.Index, p.Up, nil
			}
		}
		if p.LAG != "" && strings.EqualFold(p.LAG, iface) {
			if p.Up {
				return p.Index, true, nil
			}
			if best == 0 {
				best = p.Index
			}
		}
	}
	if best != 0 {
		return best, false, nil
	}
	return 0, false, fmt.Errorf("cisco-iosxe: configured uplink %q has no mapped physical port or aggregate members", iface)
}
