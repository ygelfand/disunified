package unifiapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/TechBlueprints/disunified/internal/devicemodel"
)

type nameObservation struct {
	Interface  string `json:"interface"`
	Device     string `json:"device"`
	Controller string `json:"controller"`
}

// nextNameObservation consumes the device observation before the controller
// read. If both changed, the later controller observation wins. Initial
// mismatches preserve controller edits, using normal default-name seeding.
func nextNameObservation(previous nameObservation, port devicemodel.Port, deviceName, controllerName string, isDefault bool) (nameObservation, bool) {
	write := controllerName != deviceName && isDefault
	if previous.Interface == port.IfName && previous.Interface != "" {
		write = deviceName != previous.Device && controllerName == previous.Controller && controllerName != deviceName
	}
	next := nameObservation{Interface: port.IfName, Device: deviceName, Controller: controllerName}
	if write {
		next.Controller = deviceName
	}
	return next, write
}

func (c *Client) loadNameState() error {
	if c.nameState != nil {
		return nil
	}
	state := map[int]nameObservation{}
	if c.NameStatePath != "" {
		data, err := os.ReadFile(c.NameStatePath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("load name sync state: %w", err)
		}
		if err == nil {
			if err := json.Unmarshal(data, &state); err != nil {
				return fmt.Errorf("decode name sync state: %w", err)
			}
		}
	}
	if state == nil {
		state = map[int]nameObservation{}
	}
	c.nameState = state
	return nil
}

func (c *Client) saveNameState(state map[int]nameObservation) error {
	if sameJSON(state, c.nameState) {
		return nil
	}
	if c.NameStatePath != "" {
		data, err := json.Marshal(state)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(c.NameStatePath), 0700); err != nil {
			return err
		}
		file, err := os.CreateTemp(filepath.Dir(c.NameStatePath), ".names-*")
		if err != nil {
			return err
		}
		defer os.Remove(file.Name())
		_, writeErr := file.Write(data)
		closeErr := file.Close()
		if err := errors.Join(writeErr, closeErr); err != nil {
			return err
		}
		if err := os.Rename(file.Name(), c.NameStatePath); err != nil {
			return err
		}
	}
	c.nameState = state
	return nil
}
