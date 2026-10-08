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
	"testing"

	"github.com/lucasssvaz/traygolin/internal/olm"
	"github.com/lucasssvaz/traygolin/internal/pangolin"
	"github.com/lucasssvaz/traygolin/internal/poller"
	"github.com/lucasssvaz/traygolin/internal/server"
)

func TestStartAfterCloseDoesNotShowAnIcon(t *testing.T) {
	// The app starts the icon in a goroutine, so turning the icon off right
	// away can call Close before Start runs. Start must then do nothing.
	// The session bus is pointed nowhere so that a Start that does try
	// fails instead of adding an icon to the real tray.
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/nonexistent/traygolin-test-bus")
	tr := &Tray{}
	if err := tr.Close(); err != nil {
		t.Fatal(err)
	}
	if err := tr.Start(); !errors.Is(err, ErrClosed) {
		t.Fatalf("Start after Close: %v, want ErrClosed", err)
	}
	if tr.item != nil {
		t.Fatal("an icon was created after Close")
	}
	// Closing twice stays harmless.
	if err := tr.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestExitNodeInUse(t *testing.T) {
	t.Parallel()
	gateway := func(resource int, sites ...int) *poller.TunnelStatus {
		peers := map[int]olm.Peer{}
		for _, id := range sites {
			peers[id] = olm.Peer{Name: "site", Connected: true}
		}
		return &poller.TunnelStatus{Running: true, Status: &olm.Status{
			Connected: true, Registered: true, GatewayActive: true,
			GatewayResource: resource, GatewaySiteIDs: sites, Peers: peers,
		}}
	}
	saved := &poller.AccountStatus{Auth: pangolin.Auth{LoggedIn: true, Active: pangolin.Account{ExitNodeResourceID: 4}}}
	for name, tc := range map[string]struct {
		st   State
		want bool
	}{
		"nothing":                 {State{}, false},
		"saved, tunnel down":      {State{Accounts: saved}, true},
		"saved, tunnel up":        {State{Accounts: saved, Tunnel: online(false)}, false},
		"active with resource":    {State{Tunnel: gateway(4, 1)}, true},
		"active without resource": {State{Tunnel: gateway(0, 1)}, true},
		"active, no sites":        {State{Tunnel: gateway(0)}, true},
		"not yet online":          {State{Tunnel: &poller.TunnelStatus{Running: true, Status: &olm.Status{GatewayActive: true}}}, false},
	} {
		if got := ExitNodeInUse(tc.st); got != tc.want {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
		}
	}
}

func TestAnActiveExitNodeIsAlwaysChoosable(t *testing.T) {
	t.Parallel()
	// The tunnel routes through an exit node it does not name, and the
	// server lists none (an older server, or the list failed). Exit nodes
	// must stay available, or the user cannot turn this one off.
	st := State{
		Tunnel: &poller.TunnelStatus{Running: true, Status: &olm.Status{
			Connected: true, Registered: true, GatewayActive: true,
			GatewaySiteIDs: []int{3}, Peers: map[int]olm.Peer{3: {Name: "Lisbon"}},
		}},
		Accounts: &poller.AccountStatus{Auth: pangolin.Auth{LoggedIn: true, Active: pangolin.Account{Server: server.Info{Version: "1.20.0"}}}},
		Server:   &poller.ServerStatus{Err: errors.New("offline")},
	}
	if reason := ExitNodesUnavailable(st); reason != "" {
		t.Fatalf("unavailable (%q) while one is in use", reason)
	}
	if got := ExitNodeLabel(st); got != "Lisbon" {
		t.Errorf("label %q", got)
	}
}

func TestEveryStateAgreesOnWhetherAnExitNodeIsInUse(t *testing.T) {
	t.Parallel()
	for i, st := range everyState() {
		inUse := ExitNodeInUse(st)
		if inUse != (ExitNodeLabel(st) != "None") {
			t.Fatalf("state %d: in use %v but labelled %q\n%+v", i, inUse, ExitNodeLabel(st), st)
		}
		if inUse && ExitNodesUnavailable(st) != "" {
			t.Fatalf("state %d: in use but unavailable: %q", i, ExitNodesUnavailable(st))
		}
	}
}
