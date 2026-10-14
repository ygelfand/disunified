package ciscoiosxe

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/TechBlueprints/disunified/internal/devicemodel"
)

type portSlot struct {
	Index    int
	Position string
	IfName   string
	Media    devicemodel.Media
}

// A hardware position survives interface-family changes on a module bay.
func portPosition(name string) string {
	i := strings.IndexAny(name, "0123456789")
	if i < 0 {
		return ""
	}
	return name[i:]
}

func (c *Collector) assignPorts(snap *devicemodel.Snapshot) error {
	if !c.slotsLoaded {
		if c.statePath != "" {
			b, err := os.ReadFile(c.statePath)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			if err == nil {
				if err := json.Unmarshal(b, &c.slots); err != nil {
					return fmt.Errorf("cisco-iosxe port state: %w", err)
				}
			}
		}
		c.slotsLoaded = true
	}
	positions := map[string]int{}
	slots := slices.Clone(c.slots)
	indexes := map[int]bool{}
	highest := 0
	for i, slot := range slots {
		if slot.Index <= 0 || slot.Index > len(slots) || slot.Position == "" || indexes[slot.Index] {
			return fmt.Errorf("cisco-iosxe: invalid persisted port slot")
		}
		if _, exists := positions[slot.Position]; exists {
			return fmt.Errorf("cisco-iosxe: duplicate persisted physical position")
		}
		positions[slot.Position] = i
		indexes[slot.Index] = true
		if slot.Index > highest {
			highest = slot.Index
		}
	}
	active := map[string]devicemodel.Port{}
	for _, p := range snap.Ports {
		position := portPosition(p.IfName)
		if _, exists := active[position]; exists {
			return fmt.Errorf("cisco-iosxe: multiple interfaces represent hardware position %s", position)
		}
		active[position] = p
		i, known := positions[position]
		if !known {
			highest++
			i = len(slots)
			slots = append(slots, portSlot{Index: highest, Position: position})
			positions[position] = i
		}
		slots[i].IfName = p.IfName
		slots[i].Media = p.Media
	}
	ports := make([]devicemodel.Port, 0, len(slots))
	for _, slot := range slots {
		p, ok := active[slot.Position]
		if !ok {
			p = devicemodel.Port{IfName: slot.IfName, Name: slot.IfName, Media: slot.Media, Lanes: 1}
		}
		p.Index = slot.Index
		ports = append(ports, p)
	}
	sort.Slice(ports, func(i, j int) bool { return ports[i].Index < ports[j].Index })
	if c.statePath != "" && !slices.Equal(slots, c.slots) {
		b, err := json.MarshalIndent(slots, "", "  ")
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(c.statePath), 0700); err != nil {
			return err
		}
		if err := os.WriteFile(c.statePath+".tmp", b, 0600); err != nil {
			return err
		}
		if err := os.Rename(c.statePath+".tmp", c.statePath); err != nil {
			return err
		}
	}
	c.slots = slots
	snap.Ports = ports
	return nil
}
