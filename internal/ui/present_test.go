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
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/lucasssvaz/traygolin/internal/metadata"
	"github.com/lucasssvaz/traygolin/internal/olm"
	"github.com/lucasssvaz/traygolin/internal/pangolin"
	"github.com/lucasssvaz/traygolin/internal/poller"
)

func TestModeText(t *testing.T) {
	cases := map[string]olm.Peer{
		"Local · same network":     {IsLocal: true},
		"Relay · through Pangolin": {IsRelay: true},
		"Direct · peer-to-peer":    {},
	}
	for want, p := range cases {
		if got := ModeText(p); got != want {
			t.Errorf("%+v: %q", p, got)
		}
	}
}

func TestLatencyAndAgo(t *testing.T) {
	if LatencyText(0) != "" || LatencyText(500*time.Microsecond) != "< 1 ms" || LatencyText(12400*time.Microsecond) != "12 ms" {
		t.Fatal("latency")
	}
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	if AgoText(time.Time{}, now) != "" || AgoText(now.Add(-time.Second), now) != "Just now" ||
		AgoText(now.Add(-30*time.Second), now) != "30 seconds ago" || AgoText(now.Add(-5*time.Minute), now) != "5 min ago" {
		t.Fatal("ago")
	}
}

func TestSitesAndClientText(t *testing.T) {
	if SitesText(nil) != "No sites" || SitesText([]olm.Peer{{Connected: true}, {}}) != "1 of 2 connected" {
		t.Fatal("sites")
	}
	if got := ClientText("0.18.1", &olm.Status{Version: "1.2.3"}); got != "Pangolin CLI 0.18.1 · olm 1.2.3" {
		t.Fatal(got)
	}
	if ClientText("", nil) != "" {
		t.Fatal("empty")
	}
}

func TestUpstreamRoundTrip(t *testing.T) {
	p, s := SplitUpstream([]string{"1.1.1.1", "8.8.8.8", "9.9.9.9"})
	if p != "1.1.1.1" || s != "8.8.8.8" || JoinUpstream(p, s) != "1.1.1.1,8.8.8.8" || JoinUpstream(" ", "") != "" {
		t.Fatal("upstream")
	}
}

func TestLoginHost(t *testing.T) {
	if LoginHost(true, "x") != metadata.CloudHost || LoginHost(false, "pangolin.example.com") != "https://pangolin.example.com" ||
		LoginHost(false, "http://lan:3000") != "http://lan:3000" || LoginHost(false, "  ") != "" {
		t.Fatal("login host")
	}
}

func TestOfflineNeeds(t *testing.T) {
	missing := &State{CLI: &poller.CLIStatus{Err: errors.New("x")}, Accounts: &poller.AccountStatus{}}
	if cli, setup, login := OfflineNeeds(missing); !cli || setup || login {
		t.Fatal("cli missing hides other steps")
	}
	noHelper := &State{CLI: &poller.CLIStatus{Path: "/usr/bin/pangolin"}, Accounts: &poller.AccountStatus{}}
	if cli, setup, login := OfflineNeeds(noHelper); cli || !setup || !login {
		t.Fatal("setup and login")
	}
	ready := &State{
		CLI:      &poller.CLIStatus{Path: "/usr/bin/pangolin", HelperInstalled: true},
		Accounts: &poller.AccountStatus{Auth: pangolin.Auth{LoggedIn: true}},
	}
	if cli, setup, login := OfflineNeeds(ready); cli || setup || login {
		t.Fatal("ready")
	}
}

func TestSidebarLayout(t *testing.T) {
	pages := map[string]bool{"offline": true, "device": true, "site:1": true, "site:2": true}
	if got := SidebarLayout(&State{Tunnel: &poller.TunnelStatus{}}, pages); !slices.Equal(got, []string{"offline"}) {
		t.Fatal(got)
	}
	st := &State{Tunnel: &poller.TunnelStatus{Running: true, Status: &olm.Status{Peers: map[int]olm.Peer{
		1: {SiteID: 1, Name: "Zeta"},
		2: {SiteID: 2, Name: "Alpha"},
		3: {SiteID: 3, Name: "NoPage"},
	}}}}
	if got := SidebarLayout(st, pages); !slices.Equal(got, []string{"device", "site:2", "site:1"}) {
		t.Fatal(got)
	}
}

func TestCopyFilled(t *testing.T) {
	v := reflect.ValueOf(Copy)
	for i := range v.NumField() {
		if v.Field(i).String() == "" {
			t.Errorf("Copy.%s is empty", v.Type().Field(i).Name)
		}
	}
}
