package ciscoiosxe

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"nemith.io/netconf"
)

type copyStartup struct {
	XMLName     xml.Name `xml:"http://cisco.com/ns/yang/Cisco-IOS-XE-rpc copy"`
	Source      string   `xml:"source-drop-node-name"`
	Destination string   `xml:"destination-drop-node-name"`
}

type copyStartupReply struct {
	netconf.RPCReply
	Result string `xml:"result"`
}

func (c *Collector) markStartupPending() error {
	if !c.saveConfig {
		return nil
	}
	c.savePending = true
	if c.savePath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(c.savePath), 0700); err != nil {
		return err
	}
	return os.WriteFile(c.savePath, []byte("pending\n"), 0600)
}

func (c *Collector) saveStartup(ctx context.Context) error {
	if !c.saveConfig || !c.savePending {
		return nil
	}
	if c.savePath != "" {
		if err := os.MkdirAll(filepath.Dir(c.savePath), 0700); err != nil {
			return err
		}
		if err := os.WriteFile(c.savePath, []byte("pending\n"), 0600); err != nil {
			return fmt.Errorf("record pending startup save: %w", err)
		}
	}
	transport, ok := c.t.(interface {
		RPC(context.Context, any, any) error
	})
	if !ok {
		return fmt.Errorf("cisco-iosxe: transport does not support saving startup configuration")
	}
	if capabilities, ok := c.t.(interface{ SupportsModule(string) bool }); ok && !capabilities.SupportsModule("Cisco-IOS-XE-rpc") {
		return fmt.Errorf("cisco-iosxe: server does not advertise Cisco-IOS-XE-rpc for startup save")
	}
	var reply copyStartupReply
	if err := transport.RPC(ctx, copyStartup{Source: "running-config", Destination: "startup-config"}, &reply); err != nil {
		return fmt.Errorf("cisco-iosxe save running to startup: %w", err)
	}
	result := strings.ToLower(strings.TrimSpace(reply.Result))
	if result != "rpc request successful" && !strings.HasPrefix(result, "[ok") {
		return fmt.Errorf("cisco-iosxe startup save was not confirmed: %s", reply.Result)
	}
	if c.savePath != "" {
		if err := os.Remove(c.savePath); err != nil {
			return fmt.Errorf("clear pending startup save: %w", err)
		}
	}
	c.savePending = false
	return nil
}
