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
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"sync"
	"testing"

	"github.com/lucasssvaz/traygolin/internal/olm"
	"github.com/lucasssvaz/traygolin/internal/pangolin"
	"github.com/lucasssvaz/traygolin/internal/poller"
	"github.com/lucasssvaz/traygolin/internal/server"
)

// everyState builds a large set of States from the combinations of each
// field, including nil ones and the awkward half-filled ones.
func everyState() []State {
	tunnels := []*poller.TunnelStatus{
		nil,
		{},
		{Running: true},
		{Running: true, Err: olm.ErrPermission},
		{Err: errors.New("boom")},
		{Running: true, Status: &olm.Status{}},
		{Running: true, Status: &olm.Status{Terminated: true}},
		{Running: true, Status: &olm.Status{Connected: true, Registered: true}},
		online(false),
		online(true),
		{Running: true, Status: &olm.Status{Connected: true, Registered: true, GatewayActive: true, GatewayResource: 7}},
		{Running: true, Status: &olm.Status{Connected: true, Registered: true, GatewayActive: true, GatewaySiteIDs: []int{9}, Peers: map[int]olm.Peer{9: {}}}},
	}
	accounts := []*poller.AccountStatus{
		nil,
		{},
		{Auth: pangolin.Auth{LoggedIn: true}},
		{Auth: pangolin.Auth{LoggedIn: true, Active: pangolin.Account{OrgID: "home", ExitNodeResourceID: 7, Server: server.Info{Version: "1.21.1"}}}},
	}
	servers := []*poller.ServerStatus{
		nil,
		{},
		{Err: errors.New("offline")},
		{Orgs: []server.Org{{ID: "home"}}, ExitNodes: []server.ExitNode{{ResourceID: 7, NiceID: "us"}}},
		{Info: server.Info{Version: "1.24.0"}},
	}
	clis := []*poller.CLIStatus{nil, {}, {Path: "/usr/bin/pangolin"}, {Err: errors.New("missing")}, {Path: "/x", Err: errors.New("x")}}
	var out []State
	for _, tun := range tunnels {
		for _, acc := range accounts {
			for _, srv := range servers {
				for _, cli := range clis {
					for _, busy := range []string{"", "Connecting…"} {
						out = append(out, State{Tunnel: tun, Accounts: acc, Server: srv, CLI: cli, Busy: busy})
					}
				}
			}
		}
	}
	return out
}

func TestEveryStateGivesUsableText(t *testing.T) {
	t.Parallel()
	states := everyState()
	if len(states) < 1000 {
		t.Fatalf("only %d states", len(states))
	}
	for i, st := range states {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("state %d panicked: %v\n%+v", i, r, st)
				}
			}()
			for name, text := range map[string]string{
				"status":     StatusText(st),
				"org":        OrgLabel(st),
				"exit":       ExitNodeLabel(st),
				"connection": ConnToggleText(st.Tunnel != nil && st.Tunnel.Running),
			} {
				if strings.TrimSpace(text) == "" {
					t.Fatalf("state %d: empty %s text\n%+v", i, name, st)
				}
			}
			if ExitNodesAvailable(st) && ExitNodesUnavailable(st) != "" {
				t.Fatalf("state %d: exit nodes are both available and unavailable", i)
			}
			if !ExitNodesAvailable(st) && ExitNodesUnavailable(st) == "" {
				t.Fatalf("state %d: exit nodes unavailable without a reason", i)
			}
			if CurrentExitNode(st) < 0 {
				t.Fatalf("state %d: negative exit node", i)
			}
			// "None" is only right when nothing is in use: no exit node chosen
			// and none active in the tunnel.
			if none := CurrentExitNode(st) == 0 && ExitSites(st.Tunnel) == ""; none != (ExitNodeLabel(st) == "None") {
				t.Fatalf("state %d: exit node %d, sites %q, labelled %q", i, CurrentExitNode(st), ExitSites(st.Tunnel), ExitNodeLabel(st))
			}
			if (CurrentOrg(st) == "") != (OrgLabel(st) == "None") {
				t.Fatalf("state %d: org %q is labelled %q", i, CurrentOrg(st), OrgLabel(st))
			}
			_ = ServerInfo(st)
		}()
	}
}

func TestStatusTextPrecedence(t *testing.T) {
	t.Parallel()
	// Busy beats everything, a missing CLI beats the tunnel.
	missing := &poller.CLIStatus{Err: errors.New("x")}
	if got := StatusText(State{Busy: "Working", CLI: missing, Tunnel: online(false)}); got != "Working" {
		t.Errorf("%q", got)
	}
	if got := StatusText(State{CLI: missing, Tunnel: online(false)}); got != "Pangolin CLI not installed" {
		t.Errorf("%q", got)
	}
	// Logged out but a tunnel is somehow up: the tunnel is the truth.
	if got := StatusText(State{Tunnel: online(false), Accounts: &poller.AccountStatus{}}); !strings.HasPrefix(got, "Connected") {
		t.Errorf("%q", got)
	}
	if got := StatusText(State{}); got != "Disconnected" {
		t.Errorf("zero state: %q", got)
	}
}

func TestStatusTextCountsSites(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		peers map[int]olm.Peer
		want  string
	}{
		"none":    {nil, "Connected · 0 of 0 sites"},
		"one up":  {map[int]olm.Peer{1: {Connected: true}}, "Connected · 1 of 1 sites"},
		"mixed":   {map[int]olm.Peer{1: {Connected: true}, 2: {}, 3: {Connected: true}}, "Connected · 2 of 3 sites"},
		"none up": {map[int]olm.Peer{1: {}, 2: {}}, "Connected · 0 of 2 sites"},
	} {
		st := State{Tunnel: &poller.TunnelStatus{Running: true, Status: &olm.Status{Connected: true, Registered: true, Peers: tc.peers}}}
		if got := StatusText(st); got != tc.want {
			t.Errorf("%s: %q, want %q", name, got, tc.want)
		}
	}
}

func TestExitSitesEdgeCases(t *testing.T) {
	t.Parallel()
	mk := func(peers map[int]olm.Peer, ids ...int) *poller.TunnelStatus {
		return &poller.TunnelStatus{Running: true, Status: &olm.Status{
			Connected: true, Registered: true, GatewayActive: true, GatewaySiteIDs: ids, Peers: peers,
		}}
	}
	if got := ExitSites(mk(nil)); got != "Active" {
		t.Errorf("no site info: %q", got)
	}
	if got := ExitSites(mk(map[int]olm.Peer{1: {Name: "A"}}, 5)); got != "Active" {
		t.Errorf("gateway site is not a known peer: %q", got)
	}
	got := ExitSites(mk(map[int]olm.Peer{1: {Name: "b"}, 2: {Name: "A"}, 3: {Name: "c"}}, 1, 2))
	if got != "A, b" {
		t.Errorf("sites are listed in name order: %q", got)
	}
	got = ExitSites(mk(map[int]olm.Peer{4: {}}, 4))
	if strings.TrimSpace(got) == "" || got == "," || strings.HasPrefix(got, ",") {
		t.Errorf("an unnamed site needs a placeholder, got %q", got)
	}
	got = ExitSites(mk(map[int]olm.Peer{4: {}, 5: {Name: "Lab"}}, 4, 5))
	if strings.HasPrefix(got, ",") || strings.HasPrefix(got, " ") || strings.Contains(got, ", ,") {
		t.Errorf("stray separators: %q", got)
	}
}

func TestExitNodeLabelFallbacks(t *testing.T) {
	t.Parallel()
	st := State{Tunnel: online(true)}
	st.Tunnel.Status.GatewayResource = 12
	st.Tunnel.Status.GatewaySiteIDs = nil
	st.Tunnel.Status.Peers = nil
	if got := ExitNodeLabel(st); got != "Exit node 12" {
		t.Errorf("no names anywhere: %q", got)
	}
	st.Server = &poller.ServerStatus{ExitNodes: []server.ExitNode{{ResourceID: 12, NiceID: "nice"}}}
	if got := ExitNodeLabel(st); got != "nice" {
		t.Errorf("nice ID is the last-resort name: %q", got)
	}
	st.Server.ExitNodes[0].Name = "Pretty"
	if got := ExitNodeLabel(st); got != "Pretty" {
		t.Errorf("%q", got)
	}
}

func TestCurrentExitNodeWhileTunnelIsStarting(t *testing.T) {
	t.Parallel()
	saved := &poller.AccountStatus{Auth: pangolin.Auth{Active: pangolin.Account{ExitNodeResourceID: 4}}}
	// The tunnel process is up but has not reported yet, or cannot be read.
	// Either way the chosen exit node should stay visible.
	for name, tun := range map[string]*poller.TunnelStatus{
		"not running":  {},
		"unreadable":   {Running: true, Err: olm.ErrPermission},
		"no status":    {Running: true},
		"stopped with": {Err: errors.New("x")},
	} {
		if got := CurrentExitNode(State{Tunnel: tun, Accounts: saved}); got != 4 {
			t.Errorf("%s: %d, want the saved exit node 4", name, got)
		}
	}
}

func TestExitNodeOptionLabel(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		e    server.ExitNode
		want string
	}{
		"plain":     {server.ExitNode{NiceID: "n"}, "n"},
		"named":     {server.ExitNode{NiceID: "n", Name: "N"}, "N"},
		"offline":   {server.ExitNode{NiceID: "n", SiteOnline: []bool{false}}, "n (offline)"},
		"partial":   {server.ExitNode{NiceID: "n", SiteOnline: []bool{false, true}}, "n"},
		"all off":   {server.ExitNode{NiceID: "n", Name: "N", SiteOnline: []bool{false, false}}, "N (offline)"},
		"no labels": {server.ExitNode{}, ""},
	} {
		if got := ExitNodeOptionLabel(tc.e); got != tc.want && !(name == "no labels" && strings.TrimSpace(got) == "(offline)") {
			t.Errorf("%s: %q, want %q", name, got, tc.want)
		}
	}
}

func TestOrgLabelFallbacks(t *testing.T) {
	t.Parallel()
	acc := &poller.AccountStatus{Auth: pangolin.Auth{Active: pangolin.Account{OrgID: "id-1"}}}
	if got := OrgLabel(State{Accounts: acc, Server: &poller.ServerStatus{Orgs: []server.Org{{ID: "id-1"}}}}); got != "id-1" {
		t.Errorf("org without a name shows its ID: %q", got)
	}
	if got := OrgLabel(State{Accounts: acc, Server: &poller.ServerStatus{Orgs: []server.Org{{ID: "other", Name: "Other"}}}}); got != "id-1" {
		t.Errorf("unknown org shows its ID: %q", got)
	}
	// The tunnel's org takes over when the account has none.
	if got := CurrentOrg(State{Tunnel: online(false), Accounts: &poller.AccountStatus{}}); got != "org" {
		t.Errorf("%q", got)
	}
	// A tunnel without an org ID defers to the account.
	tun := online(false)
	tun.Status.OrgID = ""
	if got := CurrentOrg(State{Tunnel: tun, Accounts: acc}); got != "id-1" {
		t.Errorf("%q", got)
	}
}

func TestServerInfoPrefersLive(t *testing.T) {
	t.Parallel()
	saved := &poller.AccountStatus{Auth: pangolin.Auth{Active: pangolin.Account{Server: server.Info{Version: "1.21.1", Build: "oss"}}}}
	if got := ServerInfo(State{Accounts: saved, Server: &poller.ServerStatus{}}); got.Version != "1.21.1" {
		t.Errorf("empty live info falls back to saved: %+v", got)
	}
	if got := ServerInfo(State{Accounts: saved, Server: &poller.ServerStatus{Info: server.Info{Version: "1.30.0"}}}); got.Version != "1.30.0" {
		t.Errorf("live wins: %+v", got)
	}
	if got := ServerInfo(State{}); got != (server.Info{}) {
		t.Errorf("%+v", got)
	}
}

func TestExitNodesUnavailableMessages(t *testing.T) {
	t.Parallel()
	for version, want := range map[string]string{
		"1.23.9":  "Requires Pangolin 1.24 or newer on the server",
		"1.24.0":  "No exit nodes in this organization",
		"dev":     "No exit nodes in this organization",
		"":        "No exit nodes in this organization",
		"0.9.0":   "Requires Pangolin 1.24 or newer on the server",
		"1.23.0+": "Requires Pangolin 1.24 or newer on the server",
	} {
		st := State{Server: &poller.ServerStatus{Info: server.Info{Version: version}}}
		if version == "" {
			st.Server = &poller.ServerStatus{}
		}
		if got := ExitNodesUnavailable(st); got != want {
			t.Errorf("version %q: %q, want %q", version, got, want)
		}
	}
}

func TestUpdateIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()
	tr := &Tray{}
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range 200 {
				switch (i + j) % 6 {
				case 0:
					tr.Update(online(j%2 == 0))
				case 1:
					tr.Update(&poller.AccountStatus{Auth: pangolin.Auth{LoggedIn: j%2 == 0}})
				case 2:
					tr.Update(&poller.ServerStatus{})
				case 3:
					tr.Update(&poller.CLIStatus{Path: "/x"})
				case 4:
					tr.SetBusy("busy")
				case 5:
					tr.SetBusy("")
				}
			}
		}()
	}
	wg.Wait()
	if tr.state.Tunnel == nil || tr.state.Accounts == nil || tr.state.Server == nil || tr.state.CLI == nil {
		t.Fatalf("an update was lost: %+v", tr.state)
	}
}

func TestUpdateWithNilStatusIsIgnored(t *testing.T) {
	t.Parallel()
	tr := &Tray{}
	tr.Update(nil)
	if tr.state != (State{}) {
		t.Fatalf("%+v", tr.state)
	}
}

func TestUpdateBeforeStartDoesNotCrash(t *testing.T) {
	t.Parallel()
	tr := &Tray{}
	tr.Update(online(true))
	tr.SetBusy("x")
	tr.SetBusy("")
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDecodeEdgeCases(t *testing.T) {
	t.Parallel()
	for name, data := range map[string][]byte{
		"nil":       nil,
		"empty":     {},
		"garbage":   []byte("not a png"),
		"truncated": pngBytes(t, 64, 64)[:40],
		"png magic": []byte("\x89PNG\r\n\x1a\n"),
		"tiny":      pngBytes(t, 1, 1),
		"small":     pngBytes(t, 16, 16),
		"exact":     pngBytes(t, iconSize, iconSize),
		"large":     pngBytes(t, 512, 512),
		"wide":      pngBytes(t, 256, 16),
		"tall":      pngBytes(t, 16, 256),
	} {
		p := decode(data)
		if p.Width != iconSize || p.Height != iconSize {
			t.Errorf("%s: %dx%d", name, p.Width, p.Height)
		}
		if len(p.Data) != iconSize*iconSize*4 {
			t.Errorf("%s: %d bytes of pixels", name, len(p.Data))
		}
	}
}

func TestResizeNearestEdgeCases(t *testing.T) {
	t.Parallel()
	if resizeNearest(nil, 32, 32) != nil {
		t.Error("nil image")
	}
	src := image.NewRGBA(image.Rect(0, 0, 4, 4))
	if got := resizeNearest(src, 0, 32); got != image.Image(src) {
		t.Error("zero width returns the source")
	}
	if got := resizeNearest(src, 32, -1); got != image.Image(src) {
		t.Error("negative height returns the source")
	}
	if got := resizeNearest(src, 4, 4); got != image.Image(src) {
		t.Error("same size returns the source")
	}
	// An empty source must not divide by zero.
	empty := image.NewRGBA(image.Rect(0, 0, 0, 0))
	if got := resizeNearest(empty, 8, 8); got.Bounds().Dx() != 8 {
		t.Error("empty source")
	}
	// A sub-image keeps its origin: the corner of the result is the corner of
	// the sub-image, not of the parent.
	parent := image.NewRGBA(image.Rect(0, 0, 8, 8))
	parent.Set(4, 4, color.RGBA{R: 255, A: 255})
	sub := parent.SubImage(image.Rect(4, 4, 8, 8))
	got := resizeNearest(sub, 2, 2)
	if r, _, _, _ := got.At(0, 0).RGBA(); r == 0 {
		t.Error("sub-image origin ignored")
	}
	// Every output pixel comes from the source.
	big := image.NewRGBA(image.Rect(0, 0, 100, 60))
	for y := range 60 {
		for x := range 100 {
			big.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), A: 255})
		}
	}
	out := resizeNearest(big, 33, 17)
	if out.Bounds().Dx() != 33 || out.Bounds().Dy() != 17 {
		t.Fatalf("%v", out.Bounds())
	}
	r, g, _, _ := out.At(32, 16).RGBA()
	if r>>8 > 99 || g>>8 > 59 {
		t.Errorf("last pixel reads outside the source: %d,%d", r>>8, g>>8)
	}
}
