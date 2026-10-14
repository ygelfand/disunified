package ciscoiosxe

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/TechBlueprints/disunified/internal/devicemodel"
)

// Fixed-port Catalyst PoE SKUs supply inline power on their copper front
// panel ports. UPOE is not advertised as IEEE 802.3bt.
var catalystPoESKU = regexp.MustCompile(`^WS-C3850-[0-9X]+[PUF]W?(?:-|$)`)

func nativePoEMode(config node) string {
	inline := config.child("power").child("inline")
	if inline.has("never") {
		return "off"
	}
	if inline.has("static") {
		return "static"
	}
	return "auto"
}

func addPoE(snapshot *devicemodel.Snapshot, native map[string]node, data, platform node) {
	models := map[int]string{}
	for _, component := range platform.all("component") {
		name := component.text("cname")
		if !strings.HasPrefix(name, "Switch") {
			continue
		}
		member, err := strconv.Atoi(strings.TrimPrefix(name, "Switch"))
		if err == nil {
			models[member] = component.text("state", "part-no")
		}
	}
	byName := map[string]node{}
	for _, name := range []string{"poe-port-detail", "poe-port"} {
		for _, port := range data.all(name) {
			byName[canonicalInterface(port.text("intf-name"))] = port
		}
	}
	for _, module := range data.all("poe-module") {
		snapshot.System.PoEBudgetW += module.number("available-power")
		snapshot.System.PoEAllocatedW += module.number("used-power")
	}
	for i := range snapshot.Ports {
		port := &snapshot.Ports[i]
		if !port.Present {
			continue
		}
		oper, found := byName[port.IfName]
		position := portOrder(port.IfName)
		model := ""
		if len(position) == 3 {
			model = models[position[0]]
		}
		fixedPSE := catalystPoESKU.MatchString(model) && len(position) == 3 && position[1] == 0 && (port.Media == devicemodel.MediaCopper1G || port.Media == devicemodel.MediaCopper10G || port.Media == devicemodel.MediaCopper2G5)
		if !found && !fixedPSE {
			continue
		}
		power := &devicemodel.PoE{Mode: nativePoEMode(native[port.IfName]), SupportsAT: fixedPSE}
		if found {
			power.Delivering = oper.text("oper-state") == "on"
			power.MaxPowerW = oper.number("power-admin-max")
			power.SupportsAT = power.SupportsAT || power.MaxPowerW >= 30
			power.Class = oper.text("pd-class")
			if oper.has("power-consumption") {
				power.PowerW, power.HasMetering = oper.number("power-consumption"), true
			} else if oper.has("oper-power") {
				power.PowerW, power.HasMetering = oper.number("oper-power"), true
			}
			if native[port.IfName].child("power").XMLName.Local == "" {
				if strings.HasSuffix(oper.text("admin-state"), "never") || strings.HasSuffix(oper.text("admin-state"), "off") {
					power.Mode = "off"
				}
				if strings.HasSuffix(oper.text("admin-state"), "static") {
					power.Mode = "static"
				}
			}
		}
		port.PoE = power
	}
}

func poeEdit(mode string, config node) string {
	inline := config.child("power").child("inline")
	start := `<power xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-power"><inline>`
	if mode == "off" {
		return start + `<never/></inline></power>`
	}
	// Removing never returns the port to default auto mode; adding an
	// explicit auto container introduces an unnecessary second mode edit.
	if inline.has("never") {
		start += `<never ` + operationNS + ` nc:operation="remove"/>`
	}
	return start + `</inline></power>`
}
