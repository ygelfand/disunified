// Package netconfrun connects to NETCONF servers over SSH or mutual TLS.
// Device-specific filters and YANG models belong to the caller.
package netconfrun

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
	"nemith.io/netconf"
	"nemith.io/netconf/rpc"
	"nemith.io/netconf/transport"
	ncssh "nemith.io/netconf/transport/ssh"
	nctls "nemith.io/netconf/transport/tls"
)

// Config defines the connection and authentication; TLS always verifies peers.
type Config struct {
	Transport, Address, Username, Password string
	SSHKey, KnownHosts                     string
	TLSCert, TLSKey, TLSCA, TLSServerName  string
	Timeout                                time.Duration
}

// Client serializes RPCs and reconnects after a transport or RPC failure.
type Client struct {
	mu        sync.Mutex
	cfg       Config
	session   *netconf.Session
	transport transport.Transport
}

// SupportsFeature reports a YANG module feature advertised by the connected
// server. It returns false before a connection has been established.
func (c *Client) SupportsFeature(module, feature string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session == nil {
		return false
	}
	for capability := range c.session.ServerCaps().All() {
		u, err := url.Parse(capability)
		if err != nil || u.Query().Get("module") != module {
			continue
		}
		for _, name := range strings.Split(u.Query().Get("features"), ",") {
			if name == feature {
				return true
			}
		}
	}
	return false
}

// SupportsModule reports a module advertised by the connected server.
func (c *Client) SupportsModule(module string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session == nil {
		return false
	}
	for capability := range c.session.ServerCaps().All() {
		u, err := url.Parse(capability)
		if err == nil && u.Query().Get("module") == module {
			return true
		}
	}
	return false
}

// RPC executes a vendor RPC without retrying a possibly completed operation.
func (c *Client) RPC(ctx context.Context, operation, reply any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	if err := c.connect(ctx); err != nil {
		return err
	}
	if err := c.session.Exec(ctx, operation, reply); err != nil {
		_ = c.disconnect()
		return err
	}
	return nil
}

// New validates configuration without contacting the device.
func New(cfg Config) (*Client, error) {
	if cfg.Transport == "" {
		cfg.Transport = "ssh"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 20 * time.Second
	}
	switch cfg.Transport {
	case "ssh":
		if cfg.Username == "" {
			return nil, errors.New("netconf: SSH username is required")
		}
	case "tls":
		if cfg.TLSCert == "" || cfg.TLSKey == "" {
			return nil, errors.New("netconf: TLS requires a client certificate and key")
		}
	default:
		return nil, fmt.Errorf("netconf: unknown transport %q", cfg.Transport)
	}
	if _, _, err := net.SplitHostPort(cfg.Address); err != nil {
		return nil, fmt.Errorf("netconf address: %w", err)
	}
	return &Client{cfg: cfg}, nil
}

// Get reads configuration and operational data matching a subtree filter.
func (c *Client) Get(ctx context.Context, filter string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	if err := c.connect(ctx); err != nil {
		return nil, err
	}
	op := rpc.Get{Filter: rpc.SubtreeFilter(filter)}
	data, err := op.Exec(ctx, c.session)
	if err != nil {
		_ = c.disconnect() // The RPC error is the actionable failure.
	}
	return data, err
}

// GetConfig reads the running configuration matching a subtree filter.
func (c *Client) GetConfig(ctx context.Context, filter string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	if err := c.connect(ctx); err != nil {
		return nil, err
	}
	data, err := (rpc.GetConfig{Source: rpc.Running, Filter: rpc.SubtreeFilter(filter)}).Exec(ctx, c.session)
	if err != nil {
		_ = c.disconnect() // The RPC error is the actionable failure.
	}
	return data, err
}

// Edit merges only the supplied configuration, with validation and rollback.
// A failed or timed-out write is never retried here; callers must read back.
func (c *Client) Edit(ctx context.Context, config string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	if err := c.connect(ctx); err != nil {
		return err
	}
	if !c.session.ServerCaps().Has("urn:ietf:params:netconf:capability:writable-running:1.0") || !c.session.ServerCaps().Has("urn:ietf:params:netconf:capability:rollback-on-error:1.0") {
		return errors.New("netconf: writes require writable-running and rollback-on-error")
	}
	if !c.session.ServerCaps().Has("urn:ietf:params:netconf:capability:validate:1.0") && !c.session.ServerCaps().Has("urn:ietf:params:netconf:capability:validate:1.1") {
		return errors.New("netconf: writes require validation support")
	}
	err := (rpc.EditConfig{Target: rpc.Running, DefaultOperation: rpc.MergeConfig, TestOption: rpc.TestThenSet, ErrorOption: rpc.RollbackOnError, Config: config}).Exec(ctx, c.session)
	if err != nil {
		_ = c.disconnect() // Do not hide the write error behind cleanup.
	}
	return err
}

// Close releases the transport and ends the session.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.disconnect()
}

func (c *Client) disconnect() error {
	c.session = nil
	if c.transport == nil {
		return nil
	}
	err := c.transport.Close()
	c.transport = nil
	return err
}

func (c *Client) connect(ctx context.Context) error {
	if c.session != nil {
		return nil
	}
	var tr transport.Transport
	var err error
	if c.cfg.Transport == "tls" {
		cfg, e := tlsConfig(c.cfg)
		if e != nil {
			return e
		}
		tr, err = nctls.Dial(ctx, "tcp", c.cfg.Address, cfg)
	} else {
		cfg, cleanup, e := sshConfig(c.cfg)
		if e != nil {
			return e
		}
		defer cleanup()
		tr, err = ncssh.Dial(ctx, "tcp", c.cfg.Address, cfg)
	}
	if err != nil {
		return fmt.Errorf("netconf %s dial: %w", c.cfg.Transport, err)
	}
	stop := context.AfterFunc(ctx, func() { _ = tr.Close() })
	defer stop()
	s, err := netconf.NewSession(tr, netconf.WithHelloTimeout(c.cfg.Timeout), netconf.WithLogger(slog.New(slog.DiscardHandler)))
	if err != nil {
		_ = tr.Close()
		return fmt.Errorf("netconf hello: %w", err)
	}
	if ctx.Err() != nil {
		_ = tr.Close()
		return ctx.Err()
	}
	c.transport, c.session = tr, s
	return nil
}

func sshConfig(cfg Config) (*ssh.ClientConfig, func(), error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil, err
	}
	kh := cfg.KnownHosts
	if kh == "" {
		kh = filepath.Join(home, ".ssh", "known_hosts")
	}
	verify, err := knownhosts.New(kh)
	if err != nil {
		return nil, nil, err
	}
	var auth []ssh.AuthMethod
	cleanup := func() {}
	if cfg.Password != "" {
		auth = append(auth, ssh.Password(cfg.Password))
	}
	if cfg.SSHKey == "" && os.Getenv("SSH_AUTH_SOCK") != "" {
		conn, e := net.DialTimeout("unix", os.Getenv("SSH_AUTH_SOCK"), cfg.Timeout)
		if e == nil {
			auth = append(auth, ssh.PublicKeysCallback(agent.NewClient(conn).Signers))
			cleanup = func() { _ = conn.Close() }
		}
	}
	keys := []string{cfg.SSHKey}
	if cfg.SSHKey == "" {
		keys = []string{filepath.Join(home, ".ssh", "id_ed25519"), filepath.Join(home, ".ssh", "id_ecdsa"), filepath.Join(home, ".ssh", "id_rsa")}
	}
	for _, file := range keys {
		pem, e := os.ReadFile(file)
		if e != nil {
			if cfg.SSHKey != "" {
				cleanup()
				return nil, nil, e
			}
			continue
		}
		signer, e := ssh.ParsePrivateKey(pem)
		if e != nil {
			if cfg.SSHKey != "" {
				cleanup()
				return nil, nil, e
			}
			continue
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if len(auth) == 0 {
		cleanup()
		return nil, nil, errors.New("netconf: no usable SSH authentication")
	}
	return &ssh.ClientConfig{User: cfg.Username, Auth: auth, HostKeyCallback: verify, Timeout: cfg.Timeout}, cleanup, nil
}

func tlsConfig(cfg Config) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(cfg.TLSCert, cfg.TLSKey)
	if err != nil {
		return nil, err
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		return nil, err
	}
	if cfg.TLSCA != "" {
		pem, e := os.ReadFile(cfg.TLSCA)
		if e != nil {
			return nil, e
		}
		if !roots.AppendCertsFromPEM(pem) {
			return nil, errors.New("netconf: TLS CA file contains no certificates")
		}
	}
	name := cfg.TLSServerName
	if name == "" {
		name, _, err = net.SplitHostPort(cfg.Address)
		if err != nil {
			return nil, err
		}
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}, RootCAs: roots, ServerName: name}, nil
}
