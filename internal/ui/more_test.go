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

package ui

import (
	"fmt"
	"math/rand"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucasssvaz/traygolin/internal/olm"
	"github.com/lucasssvaz/traygolin/internal/pangolin"
	"github.com/lucasssvaz/traygolin/internal/poller"
	"github.com/lucasssvaz/traygolin/internal/server"
)

func TestUpstreamSurvivesAPreferencesRoundTrip(t *testing.T) {
	// Opening Preferences and applying either field writes back what the
	// fields show. Whatever the CLI had must come back unchanged.
	r := rand.New(rand.NewSource(11))
	for range 2000 {
		vals := make([]string, r.Intn(7))
		for i := range vals {
			vals[i] = fmt.Sprintf("%d.%d.%d.%d", r.Intn(256), r.Intn(256), r.Intn(256), r.Intn(256))
		}
		p, s := SplitUpstream(vals)
		if got, want := JoinUpstream(p, s), strings.Join(vals, ","); got != want {
			t.Fatalf("%q came back as %q", want, got)
		}
	}
}

func TestJoinUpstreamAcceptsListsInEitherField(t *testing.T) {
	for _, tc := range []struct{ p, s, want string }{
		{"1.1.1.1, 2.2.2.2", "", "1.1.1.1,2.2.2.2"},
		{"", " ,3.3.3.3,", "3.3.3.3"},
		{"1.1.1.1,", ",2.2.2.2", "1.1.1.1,2.2.2.2"},
		{" , ", " ,, ", ""},
	} {
		if got := JoinUpstream(tc.p, tc.s); got != tc.want {
			t.Errorf("JoinUpstream(%q, %q) = %q, want %q", tc.p, tc.s, got, tc.want)
		}
	}
}

func TestFolderURI(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ path, dir string }{
		{"/home/u/.config/pangolin/logs/client.log", "/home/u/.config/pangolin/logs"},
		{"/home/u/my logs/client.log", "/home/u/my logs"},
		{"/tmp/odd#dir?/100%/client.log", "/tmp/odd#dir?/100%"},
		{"/tmp/ünïcode/client.log", "/tmp/ünïcode"},
		{"/client.log", "/"},
		{"client.log", wd},
		{"logs/client.log", filepath.Join(wd, "logs")},
	} {
		uri := folderURI(tc.path)
		u, err := url.Parse(uri)
		if err != nil {
			t.Errorf("%q: %q does not parse: %v", tc.path, uri, err)
			continue
		}
		if u.Scheme != "file" || u.Host != "" || u.RawQuery != "" || u.Fragment != "" {
			t.Errorf("%q: %q is not a plain file URI", tc.path, uri)
		}
		if u.Path != tc.dir {
			t.Errorf("%q: %q points at %q, want %q", tc.path, uri, u.Path, tc.dir)
		}
	}
}

// unnamedGateway is a tunnel routing through an exit node without saying
// which resource it is.
func unnamedGateway() *poller.TunnelStatus {
	return &poller.TunnelStatus{Running: true, Status: &olm.Status{
		Connected: true, Registered: true, GatewayActive: true,
		GatewaySiteIDs: []int{3}, Peers: map[int]olm.Peer{3: {Name: "Lisbon", Connected: true}},
	}}
}

func TestExitChoicesShowAnUnnamedActiveExitNode(t *testing.T) {
	loggedIn := &poller.AccountStatus{Auth: pangolin.Auth{LoggedIn: true}}
	nodes := &poller.ServerStatus{ExitNodes: []server.ExitNode{{ResourceID: 7, NiceID: "us", Name: "US East"}}}
	for name, srv := range map[string]*poller.ServerStatus{"no server": nil, "server lists others": nodes} {
		st := &State{Tunnel: unnamedGateway(), Accounts: loggedIn, Server: srv}
		choices, sel := ExitChoices(st)
		if sel == 0 {
			t.Errorf("%s: None is selected while traffic goes through an exit node: %+v", name, choices)
			continue
		}
		if choices[sel].Label != "Lisbon" || choices[sel].ID != "" {
			t.Errorf("%s: selected %+v", name, choices[sel])
		}
	}
}

func TestFindPeerUsesTheReportedSiteID(t *testing.T) {
	// The peer under key 3 says it is site 5. Its page is made as site 5
	// (from SortedPeers), so the lookup must find it under 5, or the page is
	// dropped and rebuilt on every refresh.
	st := &State{Tunnel: &poller.TunnelStatus{Running: true, Status: &olm.Status{
		Connected: true, Registered: true,
		Peers: map[int]olm.Peer{3: {SiteID: 5, Name: "Moved"}, 8: {Name: "Keyed"}},
	}}}
	for _, p := range st.Tunnel.Peers() {
		got, ok := findPeer(st, p.SiteID)
		if !ok || got.Name != p.Name {
			t.Errorf("page for site %d (%s) finds %+v, %v", p.SiteID, p.Name, got, ok)
		}
	}
	if _, ok := findPeer(st, 3); ok {
		t.Error("found a site by a map key that is not its ID")
	}
	if _, ok := findPeer(&State{}, 1); ok {
		t.Error("found a site without a tunnel")
	}
}

func TestExitActionState(t *testing.T) {
	nodes := &poller.ServerStatus{ExitNodes: []server.ExitNode{{ResourceID: 7, NiceID: "us"}}}
	saved := func(id int) *poller.AccountStatus {
		return &poller.AccountStatus{Auth: pangolin.Auth{LoggedIn: true, Active: pangolin.Account{ExitNodeResourceID: id}}}
	}
	for name, tc := range map[string]struct {
		st   State
		want string
	}{
		"nothing":        {State{}, ""},
		"listed":         {State{Server: nodes, Accounts: saved(7)}, "us"},
		"unlisted saved": {State{Server: nodes, Accounts: saved(99)}, unknownExitNode},
		"unnamed active": {State{Server: nodes, Tunnel: unnamedGateway()}, unknownExitNode},
		"none saved":     {State{Server: nodes, Accounts: saved(0)}, ""},
	} {
		if got := ExitActionState(&tc.st); got != tc.want {
			t.Errorf("%s: %q, want %q", name, got, tc.want)
		}
	}
	// The sentinel must never be mistaken for a real choice.
	choices, _ := ExitChoices(&State{Server: nodes, Accounts: saved(99)})
	for _, c := range choices {
		if c.ID == unknownExitNode {
			t.Fatal("the unknown state matches a menu item")
		}
	}
}
