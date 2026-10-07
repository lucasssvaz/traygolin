// Copyright 2026 Lucas Saavedra Vaz
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package olm talks to the local Pangolin tunnel process over its HTTP
// Unix socket. It is an independent implementation of the wire protocol
// and contains no code from the Pangolin CLI.
package olm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"syscall"
	"time"
)

// DefaultSocket is where `pangolin up` serves its local API on Linux.
const DefaultSocket = "/var/run/olm.sock"

// CLIAgent is the agent string reported by tunnels started by the Pangolin CLI.
const CLIAgent = "Pangolin CLI"

// ErrNotRunning means no tunnel process is listening on the socket.
var ErrNotRunning = errors.New("no Pangolin client is running")

// ErrPermission means the socket exists but this user cannot connect to it.
var ErrPermission = errors.New("permission denied on the Pangolin client socket")

// Client is a small HTTP client bound to the tunnel's Unix socket.
type Client struct {
	Socket string
	http   *http.Client
}

// New returns a client for socket, or DefaultSocket when empty.
func New(socket string) *Client {
	if socket == "" {
		socket = DefaultSocket
	}
	c := &Client{Socket: socket}
	c.http = &http.Client{
		Timeout: 4 * time.Second,
		Transport: &http.Transport{
			DisableKeepAlives: true,
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", c.Socket)
			},
		},
	}
	return c
}

// Peer is one site the tunnel is configured to reach.
type Peer struct {
	SiteID    int           `json:"siteId"`
	Name      string        `json:"name"`
	Connected bool          `json:"connected"`
	RTT       time.Duration `json:"rtt"`
	LastSeen  time.Time     `json:"lastSeen"`
	Endpoint  string        `json:"endpoint"`
	IsRelay   bool          `json:"isRelay"`
	IsLocal   bool          `json:"isLocal"`
	PeerIP    string        `json:"peerAddress"`
}

// Mode reports how this device reaches the site, in official-client wording.
func (p Peer) Mode() string {
	switch {
	case p.IsLocal:
		return "Local"
	case p.IsRelay:
		return "Relay"
	default:
		return "Direct"
	}
}

// ExitNode is the tunnel's own connection to its Pangolin exit node.
type ExitNode struct {
	Connected bool          `json:"connected"`
	RTT       time.Duration `json:"rtt"`
	LastSeen  time.Time     `json:"lastSeen"`
	Endpoint  string        `json:"endpoint"`
}

// StatusError is an error the tunnel reports about itself.
type StatusError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Status is the response of GET /status.
type Status struct {
	Connected       bool           `json:"connected"`
	Registered      bool           `json:"registered"`
	Terminated      bool           `json:"terminated"`
	Version         string         `json:"version"`
	Agent           string         `json:"agent"`
	OrgID           string         `json:"orgId"`
	Peers           map[int]Peer   `json:"peers"`
	NetworkSettings map[string]any `json:"networkSettings"`
	Error           *StatusError   `json:"error"`
	ExitNode        *ExitNode      `json:"exitNode"`
	GatewayActive   bool           `json:"gatewayActive"`
	GatewaySiteIDs  []int          `json:"gatewaySiteIds"`
	GatewayResource int            `json:"gatewaySiteResourceId"`
}

// Online reports whether the tunnel is up and registered with Pangolin.
func (s *Status) Online() bool {
	return s != nil && s.Connected && s.Registered && !s.Terminated
}

// SortedPeers returns peers ordered by name, then site ID.
func (s *Status) SortedPeers() []Peer {
	if s == nil {
		return nil
	}
	out := make([]Peer, 0, len(s.Peers))
	for id, p := range s.Peers {
		if p.SiteID == 0 {
			p.SiteID = id
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if a != b {
			return a < b
		}
		return out[i].SiteID < out[j].SiteID
	})
	return out
}

// IsGateway reports whether siteID is a selected exit node.
func (s *Status) IsGateway(siteID int) bool {
	if s == nil || !s.GatewayActive {
		return false
	}
	for _, id := range s.GatewaySiteIDs {
		if id == siteID {
			return true
		}
	}
	return false
}

// TunnelIP returns the device's tunnel address if the tunnel reports one.
func (s *Status) TunnelIP() string {
	if s == nil {
		return ""
	}
	for _, key := range []string{"tunnelIp", "tunnelIP", "tunnel_ip", "address", "ipv4Address", "ip"} {
		if v, ok := s.NetworkSettings[key]; ok {
			if str := firstString(v); str != "" {
				return str
			}
		}
	}
	for k, v := range s.NetworkSettings {
		lk := strings.ToLower(k)
		if strings.Contains(lk, "ipv4") && strings.Contains(lk, "address") {
			if str := firstString(v); str != "" {
				return str
			}
		}
	}
	return ""
}

func firstString(v any) string {
	switch t := v.(type) {
	case string:
		addr, _, _ := strings.Cut(t, "/")
		return addr
	case []any:
		for _, item := range t {
			if s, ok := item.(string); ok && s != "" {
				return firstString(s)
			}
		}
	}
	return ""
}

func (c *Client) do(ctx context.Context, method, path string, body any, want int, out any) error {
	if _, err := os.Stat(c.Socket); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrNotRunning
		}
		if errors.Is(err, fs.ErrPermission) {
			return ErrPermission
		}
	}
	var r io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://olm"+path, r)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		switch {
		case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
			return ErrPermission
		case errors.Is(err, syscall.ECONNREFUSED), errors.Is(err, syscall.ENOENT):
			return ErrNotRunning
		}
		return fmt.Errorf("olm %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if want == 0 {
		want = http.StatusOK
	}
	if resp.StatusCode != want && resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("olm %s %s: HTTP %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Running reports whether the tunnel answers its health check.
func (c *Client) Running(ctx context.Context) bool {
	return c.do(ctx, http.MethodGet, "/health", nil, 0, nil) == nil
}

// Status fetches the tunnel's status. It returns ErrNotRunning when no
// tunnel is up.
func (c *Client) Status(ctx context.Context) (*Status, error) {
	var st Status
	if err := c.do(ctx, http.MethodGet, "/status", nil, 0, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

// Exit asks the tunnel to shut down. It returns once the request is
// accepted; use WaitStopped to wait for the socket to disappear.
func (c *Client) Exit(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/exit", nil, 0, nil)
}

// WaitStopped polls until the tunnel no longer answers or ctx ends.
func (c *Client) WaitStopped(ctx context.Context) error {
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	for {
		if !c.Running(ctx) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
}

// SwitchOrg moves the running tunnel to another organization.
func (c *Client) SwitchOrg(ctx context.Context, orgID string) error {
	if orgID == "" {
		return errors.New("organization ID is required")
	}
	return c.do(ctx, http.MethodPost, "/switch-org", map[string]string{"org_id": orgID}, 0, nil)
}

// SelectGateway routes all traffic through the given sites of an exit
// node resource.
func (c *Client) SelectGateway(ctx context.Context, resourceID int, siteIDs []int) error {
	if resourceID <= 0 || len(siteIDs) == 0 {
		return errors.New("exit node resource and site IDs are required")
	}
	body := map[string]any{"siteResourceId": resourceID, "siteIds": siteIDs}
	return c.do(ctx, http.MethodPost, "/gateway/select", body, http.StatusAccepted, nil)
}

// DisableGateway stops routing all traffic through an exit node.
func (c *Client) DisableGateway(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/gateway/disable", nil, 0, nil)
}
