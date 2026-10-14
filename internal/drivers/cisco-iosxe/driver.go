// Package ciscoiosxe presents Cisco IOS XE switches through NETCONF only.
package ciscoiosxe

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/TechBlueprints/disunified/internal/devicemodel"
	"github.com/TechBlueprints/disunified/internal/netconfrun"
)

// Driver registers the IOS XE NETCONF implementation.
type Driver struct{}

func init()                 { devicemodel.RegisterDriver(Driver{}) }
func (Driver) Name() string { return "cisco-iosxe" }
func (Driver) Describe() string {
	return "Cisco IOS XE over NETCONF (SSH or mutual TLS); port monitoring and control"
}

// Open builds a transport; Start validates the required device data.
func (Driver) Open(ctx context.Context, cfg devicemodel.DriverConfig) (devicemodel.Device, error) {
	opts := cfg.Options
	module, err := parseModuleOptions(opts)
	if err != nil {
		return nil, err
	}
	transport := opts["transport"]
	if transport == "" {
		transport = "ssh"
	}
	address, user := cfg.SSH, cfg.Username
	if cfg.URL != "" {
		u, err := url.Parse(cfg.URL)
		if err != nil || u.Host == "" {
			return nil, fmt.Errorf("cisco-iosxe: URL must include scheme and host")
		}
		switch u.Scheme {
		case "netconf", "ssh":
			transport = "ssh"
		case "tls", "netconf+tls":
			transport = "tls"
		default:
			return nil, fmt.Errorf("cisco-iosxe: unsupported URL scheme %q", u.Scheme)
		}
		if opts["transport"] != "" && opts["transport"] != transport {
			return nil, fmt.Errorf("cisco-iosxe: transport conflicts with URL scheme")
		}
		address = u.Host
		if u.User != nil {
			user = u.User.Username()
		}
	} else if u, host, ok := strings.Cut(address, "@"); ok {
		user, address = u, host
	}
	if address == "" {
		return nil, fmt.Errorf("cisco-iosxe: set ssh or a NETCONF URL")
	}
	if _, _, err := net.SplitHostPort(address); err != nil {
		port := "830"
		if transport == "tls" {
			port = "6513"
		}
		address = net.JoinHostPort(strings.Trim(address, "[]"), port)
	}
	c, err := netconfrun.New(netconfrun.Config{Transport: transport, Address: address, Username: user, Password: cfg.Password, SSHKey: opts["ssh_key"], KnownHosts: opts["known_hosts"], TLSCert: opts["tls_cert"], TLSKey: opts["tls_key"], TLSCA: opts["tls_ca"], TLSServerName: opts["tls_server_name"]})
	if err != nil {
		return nil, err
	}
	collector := NewCollector(c)
	if value := opts["save_config"]; value != "" {
		collector.saveConfig, err = strconv.ParseBool(value)
		if err != nil {
			return nil, fmt.Errorf("cisco-iosxe save_config: %w", err)
		}
	}
	collector.module = module
	collector.uplink = canonicalInterface(strings.TrimSpace(opts["uplink"]))
	if dir := opts["state_dir"]; dir != "" {
		collector.statePath = filepath.Join(dir, "ports.json")
		collector.savePath = filepath.Join(dir, "startup-pending")
		collector.poeRestorePath = filepath.Join(dir, "poe-restore.json")
		if err := collector.loadPoERestores(); err != nil {
			return nil, err
		}
		if collector.saveConfig {
			_, err := os.Stat(collector.savePath)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
			collector.savePending = err == nil
		}
	}
	return collector, nil
}

var (
	_ devicemodel.Driver     = Driver{}
	_ devicemodel.Device     = (*Collector)(nil)
	_ devicemodel.Controller = (*Collector)(nil)
	_ devicemodel.Planner    = (*Collector)(nil)
	_ devicemodel.Capable    = (*Collector)(nil)
)
