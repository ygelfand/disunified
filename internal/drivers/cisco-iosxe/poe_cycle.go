package ciscoiosxe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type poeRestore struct {
	MAC     string `json:"device_mac"`
	AutoMax string `json:"auto_max_mw,omitempty"`
}

// CyclePort briefly removes inline power and restores auto mode. Restoration
// survives cancellation and restart; temporary off mode is never saved.
func (c *Collector) CyclePort(ctx context.Context, index int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.last == nil {
		return errors.New("cisco-iosxe: no snapshot")
	}
	for _, port := range c.last.Ports {
		if port.Index != index {
			continue
		}
		if !port.Present || port.PoE == nil || port.PoE.Mode != "auto" || c.last.System.MAC == "" {
			return fmt.Errorf("cisco-iosxe: power cycle requires a present PoE port in auto mode")
		}
		if c.poeRestores == nil {
			c.poeRestores = map[string]poeRestore{}
		}
		previous, existed := c.poeRestores[port.IfName]
		c.poeRestores[port.IfName] = poeRestore{MAC: c.last.System.MAC, AutoMax: c.native[port.IfName].text("power", "inline", "auto", "max")}
		if err := c.persistPoERestores(); err != nil {
			if existed {
				c.poeRestores[port.IfName] = previous
			} else {
				delete(c.poeRestores, port.IfName)
			}
			return err
		}
		edit, err := c.poeInterfaceEdit(port.IfName, "off", "")
		if err == nil {
			err = c.t.Edit(ctx, edit)
		}
		if err == nil {
			timer := time.NewTimer(2 * time.Second)
			select {
			case <-timer.C:
			case <-ctx.Done():
				err = ctx.Err()
			}
			timer.Stop()
		}
		restoreCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
		defer cancel()
		return errors.Join(err, c.restorePoE(restoreCtx))
	}
	return fmt.Errorf("cisco-iosxe: unknown port %d", index)
}

func (c *Collector) poeInterfaceEdit(iface, mode, autoMax string) (string, error) {
	family := c.native[iface].XMLName.Local
	if family == "" {
		return "", fmt.Errorf("cisco-iosxe: native interface missing for %s", iface)
	}
	power := poeEdit(mode, c.native[iface])
	if mode == "auto" && autoMax != "" {
		power = strings.Replace(power, `</inline>`, `<auto><max>`+escape(autoMax)+`</max></auto></inline>`, 1)
	}
	return `<native xmlns="` + nativeNS + `"><interface><` + family + `><name>` + escape(strings.TrimPrefix(iface, family)) + `</name>` + power + `</` + family + `></interface></native>`, nil
}

func (c *Collector) restorePoE(ctx context.Context) error {
	for iface, restore := range c.poeRestores {
		if c.last == nil || restore.MAC == "" || restore.MAC != c.last.System.MAC {
			return errors.New("cisco-iosxe: pending PoE restore belongs to a different device")
		}
		data, err := c.t.GetConfig(ctx, configFilter)
		if err != nil {
			return err
		}
		root, err := decode(data)
		if err != nil {
			return err
		}
		c.native = nativePorts(root)
		edit, err := c.poeInterfaceEdit(iface, "auto", restore.AutoMax)
		if err != nil {
			return err
		}
		if err := c.t.Edit(ctx, edit); err != nil {
			return fmt.Errorf("restore PoE on %s: %w", iface, err)
		}
		data, err = c.t.GetConfig(ctx, configFilter)
		if err != nil {
			return err
		}
		root, err = decode(data)
		if err != nil {
			return err
		}
		current := nativePorts(root)
		config, ok := current[iface]
		if !ok || nativePoEMode(config) != "auto" || (restore.AutoMax != "" && config.text("power", "inline", "auto", "max") != restore.AutoMax) {
			return fmt.Errorf("cisco-iosxe: PoE restore readback differs for %s", iface)
		}
		c.native = current
		for i, port := range c.last.Ports {
			if port.IfName == iface && port.PoE != nil {
				power := *port.PoE
				power.Mode = "auto"
				c.last.Ports[i].PoE = &power
			}
		}
		if err := c.markStartupPending(); err != nil {
			return err
		}
		if err := c.saveStartup(ctx); err != nil {
			return err
		}
		delete(c.poeRestores, iface)
		if err := c.persistPoERestores(); err != nil {
			c.poeRestores[iface] = restore
			return err
		}
	}
	return nil
}

func (c *Collector) loadPoERestores() error {
	data, err := os.ReadFile(c.poeRestorePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &c.poeRestores); err != nil {
		return fmt.Errorf("load pending PoE restores: %w", err)
	}
	return nil
}

func (c *Collector) persistPoERestores() error {
	if c.poeRestorePath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(c.poeRestorePath), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(c.poeRestores)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(c.poeRestorePath), ".poe-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	return os.Rename(file.Name(), c.poeRestorePath)
}
