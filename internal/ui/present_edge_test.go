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
	"errors"
	"html"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lucasssvaz/traygolin/internal/metadata"
	"github.com/lucasssvaz/traygolin/internal/olm"
	"github.com/lucasssvaz/traygolin/internal/pangolin"
	"github.com/lucasssvaz/traygolin/internal/poller"
	"github.com/lucasssvaz/traygolin/internal/server"
)

func TestLoginHostForms(t *testing.T) {
	for _, tc := range []struct {
		cloud  bool
		custom string
		want   string
	}{
		{true, "ignored", metadata.CloudHost},
		{true, "", metadata.CloudHost},
		{false, "", ""},
		{false, "   \t", ""},
		{false, "pangolin.example.com", "https://pangolin.example.com"},
		{false, "  pangolin.example.com  ", "https://pangolin.example.com"},
		{false, "pangolin.example.com:8443", "https://pangolin.example.com:8443"},
		{false, "10.0.0.5:3000", "https://10.0.0.5:3000"},
		{false, "[::1]:3000", "https://[::1]:3000"},
		{false, "https://pangolin.example.com/", "https://pangolin.example.com/"},
		{false, "http://lan:3000", "http://lan:3000"},
		{false, "HTTPS://Pangolin.Example.com", "HTTPS://Pangolin.Example.com"},
		// Not something `pangolin login` can talk to, and nothing that
		// could be taken for one of its options.
		{false, "ftp://example.com", ""},
		{false, "file:///etc/passwd", ""},
		{false, "javascript://x%0Aalert(1)", ""},
		{false, "--help", "https://--help"},
		{false, "--host=://x", ""},
		{false, "https://", ""},
		{false, "http://", ""},
		{false, "://nothing", ""},
		{false, "https://two words.example", ""},
		{false, "https://a.example\nhttps://b.example", ""},
		{false, "https://a.example\x00", ""},
	} {
		got := LoginHost(tc.cloud, tc.custom)
		if got != tc.want {
			t.Errorf("LoginHost(%v, %q) = %q, want %q", tc.cloud, tc.custom, got, tc.want)
		}
		if strings.HasPrefix(got, "-") {
			t.Errorf("LoginHost(%v, %q) = %q looks like an option", tc.cloud, tc.custom, got)
		}
	}
}

func TestUpdateHintQuotesThePath(t *testing.T) {
	const prefix, suffix = "<tt>sudo chown root:root ", "</tt>"
	for path, want := range map[string]string{
		"/usr/local/bin/pangolin":    "/usr/local/bin/pangolin",
		"/home/me/my tools/pangolin": "'/home/me/my tools/pangolin'",
		"/home/me/it's/pangolin":     `'/home/me/it'\''s/pangolin'`,
		"/home/me/a$b/pangolin":      "'/home/me/a$b/pangolin'",
		"/opt/a;b/pangolin":          "'/opt/a;b/pangolin'",
		"/opt/a<b>/pangolin":         "'/opt/a<b>/pangolin'",
		"/opt/a&b/pangolin":          "'/opt/a&b/pangolin'",
		"/opt/a`id`/pangolin":        "'/opt/a`id`/pangolin'",
	} {
		hint := updateHint(path)
		i := strings.Index(hint, prefix)
		j := strings.Index(hint[i+len(prefix):], suffix)
		if i < 0 || j < 0 {
			t.Errorf("updateHint(%q) has no chown command: %q", path, hint)
			continue
		}
		// What the user copies is the markup with its entities decoded.
		if got := html.UnescapeString(hint[i+len(prefix) : i+len(prefix)+j]); got != want {
			t.Errorf("updateHint(%q) offers %q, want %q", path, got, want)
		}
		// And the markup itself must be well formed: no raw angle brackets
		// from the path.
		if strings.Contains(hint[i+len(prefix):i+len(prefix)+j], "<") {
			t.Errorf("updateHint(%q) leaves a raw < in the markup: %q", path, hint)
		}
	}
}

func TestUpdateHintPackagedPaths(t *testing.T) {
	for path, packaged := range map[string]bool{
		"/usr/bin/pangolin":         true,
		"/usr/bin/":                 true,
		"/usr/bin":                  false,
		"/usr/local/bin/pangolin":   false,
		"/usr/bin2/pangolin":        false,
		"/home/me/usr/bin/pangolin": false,
		"":                          false,
	} {
		got := updateHint(path) == "Update it with your system package manager."
		if got != packaged {
			t.Errorf("updateHint(%q) packaged = %v, want %v", path, got, packaged)
		}
	}
}

func TestLatencyTextBoundaries(t *testing.T) {
	for d, want := range map[time.Duration]string{
		-time.Second:               "",
		0:                          "",
		1:                          "< 1 ms",
		999 * time.Microsecond:     "< 1 ms",
		time.Millisecond:           "1 ms",
		1499 * time.Microsecond:    "1 ms",
		1500 * time.Microsecond:    "2 ms",
		17562199 * time.Nanosecond: "18 ms",
		time.Second:                "1000 ms",
		2601796966:                 "2602 ms",
		time.Hour:                  "3600000 ms",
	} {
		if got := LatencyText(d); got != want {
			t.Errorf("LatencyText(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestAgoTextBoundaries(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		ago  time.Duration
		want string
	}{
		{0, "Just now"},
		{4*time.Second + 999*time.Millisecond, "Just now"},
		{5 * time.Second, "5 seconds ago"},
		{59 * time.Second, "59 seconds ago"},
		{time.Minute, "1 min ago"},
		{59*time.Minute + 59*time.Second, "59 min ago"},
		{-time.Minute, "Just now"}, // a clock that runs ahead of this machine's
		{-24 * time.Hour, "Just now"},
	} {
		if got := AgoText(now.Add(-tc.ago), now); got != tc.want {
			t.Errorf("%v ago: %q, want %q", tc.ago, got, tc.want)
		}
	}
	// Older than an hour is shown as a date and must not be empty.
	if got := AgoText(now.Add(-3*time.Hour), now); got == "" || strings.Contains(got, "ago") {
		t.Errorf("3 hours ago: %q", got)
	}
	if got := AgoText(now.Add(-24*365*time.Hour), now); got == "" {
		t.Errorf("a year ago: %q", got)
	}
}

func TestSitesTextCounts(t *testing.T) {
	up, down := olm.Peer{Connected: true}, olm.Peer{}
	for name, tc := range map[string]struct {
		peers []olm.Peer
		want  string
	}{
		"none":    {nil, "No sites"},
		"empty":   {[]olm.Peer{}, "No sites"},
		"all up":  {[]olm.Peer{up, up}, "2 of 2 connected"},
		"none up": {[]olm.Peer{down, down, down}, "0 of 3 connected"},
		"one":     {[]olm.Peer{up}, "1 of 1 connected"},
	} {
		if got := SitesText(tc.peers); got != tc.want {
			t.Errorf("%s: %q, want %q", name, got, tc.want)
		}
	}
}

func TestClientTextForms(t *testing.T) {
	for _, tc := range []struct {
		cli  string
		st   *olm.Status
		want string
	}{
		{"", nil, ""},
		{"  ", nil, ""},
		{"0.18.1", nil, "Pangolin CLI 0.18.1"},
		{"pangolin 0.18.1", nil, "Pangolin CLI 0.18.1"},
		{" 0.18.1\n", nil, "Pangolin CLI 0.18.1"},
		{"", &olm.Status{Version: "1.2.3"}, "olm 1.2.3"},
		{"", &olm.Status{}, ""},
		{"0.18.1", &olm.Status{}, "Pangolin CLI 0.18.1"},
		{"0.18.1", &olm.Status{Version: "1.2.3"}, "Pangolin CLI 0.18.1 · olm 1.2.3"},
	} {
		if got := ClientText(tc.cli, tc.st); got != tc.want {
			t.Errorf("ClientText(%q, %+v) = %q, want %q", tc.cli, tc.st, got, tc.want)
		}
	}
}

func TestUpstreamEdgeCases(t *testing.T) {
	for _, tc := range []struct {
		in       []string
		pri, sec string
		joined   string
	}{
		{nil, "", "", ""},
		{[]string{}, "", "", ""},
		{[]string{"1.1.1.1"}, "1.1.1.1", "", "1.1.1.1"},
		{[]string{"", "8.8.8.8"}, "", "8.8.8.8", "8.8.8.8"},
		{[]string{"1.1.1.1", "8.8.8.8"}, "1.1.1.1", "8.8.8.8", "1.1.1.1,8.8.8.8"},
		{[]string{" 1.1.1.1 ", " 8.8.8.8 "}, " 1.1.1.1 ", " 8.8.8.8 ", "1.1.1.1,8.8.8.8"},
		{[]string{"a", "b", "c", "d"}, "a", "b, c, d", "a,b,c,d"},
		{[]string{"a", "", "c"}, "a", ", c", "a,c"},
	} {
		p, s := SplitUpstream(tc.in)
		if p != tc.pri || s != tc.sec {
			t.Errorf("SplitUpstream(%q) = %q, %q", tc.in, p, s)
		}
		if got := JoinUpstream(p, s); got != tc.joined {
			t.Errorf("JoinUpstream(%q, %q) = %q, want %q", p, s, got, tc.joined)
		}
	}
}

func TestFirstLine(t *testing.T) {
	for in, want := range map[string]string{
		"":                 "",
		"  ":               "",
		"one":              "one",
		"one\ntwo":         "one",
		"one\r\ntwo":       "one",
		"\n\none\ntwo":     "one",
		"  padded  \nnext": "padded",
		"trailing\n":       "trailing",
		"unicode ✓\nnext":  "unicode ✓",
		"a\rb":             "a\rb",
		"\r\nwindows only": "windows only",
	} {
		if got := firstLine(in); got != want {
			t.Errorf("firstLine(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOfflineNeedsEveryCombination(t *testing.T) {
	clis := []*poller.CLIStatus{
		nil,
		{Err: errors.New("x")},
		{},
		{Path: "/usr/bin/pangolin"},
		{Path: "/usr/bin/pangolin", HelperInstalled: true},
	}
	accs := []*poller.AccountStatus{nil, {}, {Auth: pangolin.Auth{LoggedIn: true}}}
	for _, cli := range clis {
		for _, acc := range accs {
			st := &State{CLI: cli, Accounts: acc}
			needCLI, setup, login := OfflineNeeds(st)
			if needCLI && (setup || login) {
				t.Errorf("a missing CLI must hide the later steps: %+v %+v", cli, acc)
			}
			if cli == nil && (needCLI || setup) {
				t.Errorf("nothing is known about the CLI yet, so it cannot be missing: %+v", acc)
			}
			if acc == nil && login {
				t.Errorf("no account information yet must not ask to log in")
			}
			if cli != nil && cli.Found() && cli.HelperInstalled && setup {
				t.Errorf("helper is installed but setup is requested")
			}
		}
	}
}

func TestSidebarLayoutEdgeCases(t *testing.T) {
	pages := map[string]struct{}{"offline": {}, "device": {}, "site:1": {}, "site:2": {}, "site:10": {}}
	if got := SidebarLayout(&State{}, pages); !slices.Equal(got, []string{"offline"}) {
		t.Errorf("zero state: %v", got)
	}
	if got := SidebarLayout(&State{}, map[string]struct{}{}); got != nil {
		t.Errorf("no pages: %v", got)
	}
	running := func(peers map[int]olm.Peer) *State {
		return &State{Tunnel: &poller.TunnelStatus{Running: true, Status: &olm.Status{Peers: peers}}}
	}
	if got := SidebarLayout(running(nil), pages); !slices.Equal(got, []string{"device"}) {
		t.Errorf("no peers: %v", got)
	}
	if got := SidebarLayout(&State{Tunnel: &poller.TunnelStatus{Running: true}}, pages); !slices.Equal(got, []string{"device"}) {
		t.Errorf("running without status: %v", got)
	}
	// Peers with the same name keep a stable order, and site 10 does not
	// sort before site 2 just because of its text.
	st := running(map[int]olm.Peer{10: {Name: "same"}, 2: {Name: "same"}, 1: {Name: "same"}})
	if got := SidebarLayout(st, pages); !slices.Equal(got, []string{"device", "site:1", "site:2", "site:10"}) {
		t.Errorf("same names: %v", got)
	}
	// Only pages that exist are listed.
	if got := SidebarLayout(st, map[string]struct{}{"device": {}}); !slices.Equal(got, []string{"device"}) {
		t.Errorf("missing site pages: %v", got)
	}
	if got := SidebarLayout(st, map[string]struct{}{"site:2": {}}); !slices.Equal(got, []string{"site:2"}) {
		t.Errorf("missing device page: %v", got)
	}
}

func TestExitChoicesEdgeCases(t *testing.T) {
	nodes := &poller.ServerStatus{ExitNodes: []server.ExitNode{
		{ResourceID: 7, NiceID: "us", Name: "US East"},
		{ResourceID: 9, NiceID: "eu", SiteOnline: []bool{false}},
	}}
	saved := func(id int) *poller.AccountStatus {
		return &poller.AccountStatus{Auth: pangolin.Auth{Active: pangolin.Account{ExitNodeResourceID: id}}}
	}
	// Nothing known.
	choices, sel := ExitChoices(&State{})
	if len(choices) != 1 || choices[0].Label != "None" || choices[0].ID != "" || sel != 0 {
		t.Errorf("empty: %+v %d", choices, sel)
	}
	// None selected.
	choices, sel = ExitChoices(&State{Server: nodes, Accounts: saved(0)})
	if len(choices) != 3 || sel != 0 {
		t.Errorf("none: %+v %d", choices, sel)
	}
	// Each exit node can be selected, and its ID is the nice ID.
	for id, want := range map[int]string{7: "us", 9: "eu"} {
		choices, sel = ExitChoices(&State{Server: nodes, Accounts: saved(id)})
		if choices[sel].ID != want {
			t.Errorf("resource %d selects %+v", id, choices[sel])
		}
	}
	if choices[2].Label != "eu (offline)" {
		t.Errorf("offline label %q", choices[2].Label)
	}
	// A saved node the server does not list is kept visible and selected, but
	// cannot be chosen again (it has no ID).
	choices, sel = ExitChoices(&State{Server: nodes, Accounts: saved(55)})
	if len(choices) != 4 || sel != 3 || choices[sel].ID != "" || choices[sel].Label == "None" || choices[sel].Label == "" {
		t.Errorf("unlisted: %+v %d", choices, sel)
	}
	// Same, without any server data.
	choices, sel = ExitChoices(&State{Accounts: saved(55)})
	if len(choices) != 2 || sel != 1 {
		t.Errorf("no server: %+v %d", choices, sel)
	}
	// The index is always valid.
	for _, st := range []*State{{}, {Server: nodes}, {Accounts: saved(7)}, {Server: nodes, Accounts: saved(1000)}} {
		c, i := ExitChoices(st)
		if i < 0 || i >= len(c) {
			t.Errorf("index %d out of %d choices", i, len(c))
		}
	}
	if CurrentExitNiceID(&State{}) != "" {
		t.Error("no nice ID without a server")
	}
	if got := CurrentExitNiceID(&State{Server: nodes, Accounts: saved(9)}); got != "eu" {
		t.Errorf("nice ID %q", got)
	}
	if got := CurrentExitNiceID(&State{Server: nodes, Accounts: saved(55)}); got != "" {
		t.Errorf("unlisted node %q", got)
	}
}

func TestOrgChoicesEdgeCases(t *testing.T) {
	orgs := &poller.ServerStatus{Orgs: []server.Org{{ID: "home", Name: "Home"}, {ID: "work"}}}
	acc := func(org string) *poller.AccountStatus {
		return &poller.AccountStatus{Auth: pangolin.Auth{Active: pangolin.Account{OrgID: org}}}
	}
	choices, sel := OrgChoices(&State{})
	if len(choices) != 0 || sel != -1 {
		t.Errorf("nothing known: %+v %d", choices, sel)
	}
	choices, sel = OrgChoices(&State{Server: orgs, Accounts: acc("work")})
	if len(choices) != 2 || sel != 1 || choices[0].Label != "Home" || choices[1].Label != "work" {
		t.Errorf("listed: %+v %d", choices, sel)
	}
	// An organization the list does not contain is added rather than hidden.
	choices, sel = OrgChoices(&State{Server: orgs, Accounts: acc("gone")})
	if len(choices) != 3 || sel != 2 || choices[2].ID != "gone" {
		t.Errorf("unlisted: %+v %d", choices, sel)
	}
	// Without a list only the current organization is offered.
	choices, sel = OrgChoices(&State{Accounts: acc("home")})
	if len(choices) != 1 || sel != 0 || choices[0].ID != "home" {
		t.Errorf("no list: %+v %d", choices, sel)
	}
	// With a list but no current organization nothing is selected.
	choices, sel = OrgChoices(&State{Server: orgs, Accounts: acc("")})
	if len(choices) != 2 || sel != -1 {
		t.Errorf("no current: %+v %d", choices, sel)
	}
}
