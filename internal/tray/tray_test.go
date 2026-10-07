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

package tray

import (
	"errors"
	"image"
	"testing"
	"unique"

	"github.com/lucasssvaz/traygolin/internal/olm"
	"github.com/lucasssvaz/traygolin/internal/pangolin"
	"github.com/lucasssvaz/traygolin/internal/poller"
	"github.com/lucasssvaz/traygolin/internal/server"
)

func online(gateway bool) *poller.TunnelStatus {
	return &poller.TunnelStatus{Running: true, Status: &olm.Status{
		Connected: true, Registered: true, OrgID: "org",
		GatewayActive: gateway, GatewaySiteIDs: []int{2},
		Peers: map[int]olm.Peer{
			1: {SiteID: 1, Name: "Office", Connected: true},
			2: {SiteID: 2, Name: "Lab", Connected: false},
		},
	}}
}

func TestStatusText(t *testing.T) {
	t.Parallel()
	cases := []struct {
		st   State
		want string
	}{
		{State{CLI: &poller.CLIStatus{Err: errors.New("missing")}}, "Pangolin CLI not installed"},
		{State{Accounts: &poller.AccountStatus{}}, "Not logged in"},
		{State{Accounts: &poller.AccountStatus{Auth: pangolin.Auth{LoggedIn: true}}, Tunnel: &poller.TunnelStatus{}}, "Disconnected"},
		{State{Tunnel: online(false)}, "Connected · 1 of 2 sites"},
		{State{Tunnel: &poller.TunnelStatus{Running: true, Status: &olm.Status{}}}, "Connecting…"},
		{State{Tunnel: &poller.TunnelStatus{Running: true, Err: olm.ErrPermission}}, "Connected (status unavailable)"},
		{State{Tunnel: online(false), Busy: "Disconnecting…"}, "Disconnecting…"},
	}
	for _, c := range cases {
		if got := StatusText(c.st); got != c.want {
			t.Errorf("%+v: got %q want %q", c.st, got, c.want)
		}
	}
}

func TestConnToggleText(t *testing.T) {
	t.Parallel()
	if ConnToggleText(true) != "Disconnect" || ConnToggleText(false) != "Connect" {
		t.Fatal("toggle text")
	}
}

func TestExitSites(t *testing.T) {
	t.Parallel()
	if got := ExitSites(online(true)); got != "Lab" {
		t.Fatalf("%q", got)
	}
	if got := ExitSites(online(false)); got != "" {
		t.Fatalf("%q", got)
	}
	if got := ExitSites(nil); got != "" {
		t.Fatalf("%q", got)
	}
}

func TestCurrentOrgPrefersTunnel(t *testing.T) {
	t.Parallel()
	st := State{Tunnel: online(false), Accounts: &poller.AccountStatus{Auth: pangolin.Auth{Active: pangolin.Account{OrgID: "saved"}}}}
	if CurrentOrg(st) != "org" {
		t.Fatal(CurrentOrg(st))
	}
	st.Tunnel = &poller.TunnelStatus{}
	if CurrentOrg(st) != "saved" {
		t.Fatal(CurrentOrg(st))
	}
}

func TestOrgLabelUsesServerName(t *testing.T) {
	t.Parallel()
	st := State{Accounts: &poller.AccountStatus{Auth: pangolin.Auth{Active: pangolin.Account{OrgID: "home"}}}}
	if OrgLabel(st) != "home" {
		t.Fatal(OrgLabel(st))
	}
	st.Server = &poller.ServerStatus{Orgs: []server.Org{{ID: "home", Name: "Home Lab"}}}
	if OrgLabel(st) != "Home Lab" {
		t.Fatal(OrgLabel(st))
	}
	if OrgLabel(State{}) != "None" {
		t.Fatal(OrgLabel(State{}))
	}
}

func TestCurrentExitNode(t *testing.T) {
	t.Parallel()
	saved := &poller.AccountStatus{Auth: pangolin.Auth{Active: pangolin.Account{ExitNodeResourceID: 9}}}
	nodes := &poller.ServerStatus{ExitNodes: []server.ExitNode{
		{ResourceID: 7, NiceID: "us", Name: "US East", SiteIDs: []int{2}},
		{ResourceID: 9, NiceID: "eu", Name: "EU", SiteIDs: []int{3}, SiteOnline: []bool{false}},
	}}

	// Disconnected: the saved choice is what the next connection uses.
	st := State{Tunnel: &poller.TunnelStatus{}, Accounts: saved, Server: nodes}
	if CurrentExitNode(st) != 9 || ExitNodeLabel(st) != "EU" || !ExitNodesAvailable(st) {
		t.Fatalf("saved: %d %q", CurrentExitNode(st), ExitNodeLabel(st))
	}

	// Connected: the tunnel's active gateway wins over the saved one.
	st.Tunnel = online(true)
	st.Tunnel.Status.GatewayResource = 7
	if CurrentExitNode(st) != 7 || ExitNodeLabel(st) != "US East" {
		t.Fatalf("active: %d %q", CurrentExitNode(st), ExitNodeLabel(st))
	}
	st.Tunnel = online(false)
	if CurrentExitNode(st) != 0 || ExitNodeLabel(st) != "None" {
		t.Fatalf("none: %d %q", CurrentExitNode(st), ExitNodeLabel(st))
	}

	// Unknown to the server: fall back to the tunnel's site names.
	st = State{Tunnel: online(true)}
	st.Tunnel.Status.GatewayResource = 5
	if ExitNodeLabel(st) != "Lab" || !ExitNodesAvailable(st) {
		t.Fatalf("unknown: %q", ExitNodeLabel(st))
	}

	// Servers without exit nodes hide the controls.
	if ExitNodesAvailable(State{Tunnel: &poller.TunnelStatus{}, Server: &poller.ServerStatus{}}) {
		t.Fatal("no exit nodes should hide controls")
	}
	old := State{
		Tunnel:   &poller.TunnelStatus{},
		Accounts: &poller.AccountStatus{Auth: pangolin.Auth{Active: pangolin.Account{Server: server.Info{Version: "1.21.1"}}}},
		Server:   &poller.ServerStatus{},
	}
	if got := ExitNodesUnavailable(old); got != "Requires Pangolin 1.24 or newer on the server" {
		t.Fatalf("old server: %q", got)
	}
	old.Server.Info = server.Info{Version: "1.24.0"}
	if got := ExitNodesUnavailable(old); got != "No exit nodes in this organization" {
		t.Fatalf("live version wins: %q", got)
	}
	if ServerInfo(old).Version != "1.24.0" || ServerInfo(State{Accounts: old.Accounts}).Version != "1.21.1" {
		t.Fatal("server info fallback")
	}
	if ExitNodesUnavailable(st) != "" {
		t.Fatal("available")
	}
	if ExitNodeOptionLabel(nodes.ExitNodes[1]) != "EU (offline)" || ExitNodeOptionLabel(nodes.ExitNodes[0]) != "US East" {
		t.Fatal("option labels")
	}
}

func TestDirty(t *testing.T) {
	t.Parallel()
	tr := &Tray{prev: map[unique.Handle[string]][]any{}}
	k := unique.Make("k")
	if !tr.dirty(k, "a", true) {
		t.Fatal("first is dirty")
	}
	if tr.dirty(k, "a", true) {
		t.Fatal("same is clean")
	}
	if !tr.dirty(k, "a", false) {
		t.Fatal("change is dirty")
	}
}

func TestUpdateWithoutItemIsSafe(t *testing.T) {
	t.Parallel()
	var nilTray *Tray
	nilTray.Update(online(true))
	nilTray.SetBusy("x")
	tr := &Tray{}
	tr.Update(online(true))
	if tr.state.Tunnel == nil {
		t.Fatal("state not stored")
	}
}

func TestDecodeAndResize(t *testing.T) {
	t.Parallel()
	p := decode(nil)
	if p.Width != iconSize || p.Height != iconSize {
		t.Fatalf("%dx%d", p.Width, p.Height)
	}
	got := resizeNearest(image.NewRGBA(image.Rect(0, 0, 128, 128)), 32, 32)
	if got.Bounds().Dx() != 32 {
		t.Fatal(got.Bounds())
	}
}
