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
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"testing"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	traygolin "github.com/lucasssvaz/traygolin"
	"github.com/lucasssvaz/traygolin/internal/metadata"
	"github.com/lucasssvaz/traygolin/internal/olm"
	"github.com/lucasssvaz/traygolin/internal/pangolin"
	"github.com/lucasssvaz/traygolin/internal/poller"
	"github.com/lucasssvaz/traygolin/internal/server"
)

func gtkReady(t *testing.T) {
	t.Helper()
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		t.Skip("GTK UI tests need DISPLAY or WAYLAND_DISPLAY (CI runs them under xvfb-run)")
	}
	gtk.Init()
}

var (
	sharedAppOnce sync.Once
	sharedApp     *adw.Application
)

// testApp builds an App around one registered GtkApplication shared by
// all tests, since an application ID can only be exported once.
func testApp(t *testing.T) *App {
	t.Helper()
	gtkReady(t)
	sharedAppOnce.Do(func() {
		sharedApp = adw.NewApplication("io.github.lucasssvaz.Traygolin.test", gio.ApplicationNonUnique)
		if err := sharedApp.Register(context.Background()); err != nil {
			t.Log("register application:", err)
		}
	})
	a := &App{
		ctx: t.Context(),
		app: sharedApp,
		cli: &pangolin.Client{Binary: "/this/pangolin/does/not/exist"},
		olm: olm.New(filepath.Join(t.TempDir(), "olm.sock")),
	}
	a.initActions(t.Context())
	a.win = NewMainWindow(a)
	t.Cleanup(func() { a.win.MainWindow.Destroy() })
	return a
}

// assertFilled fails if any exported pointer field was not found in
// the UI file.
func assertFilled(t *testing.T, v any) {
	t.Helper()
	rv := reflect.ValueOf(v).Elem()
	for i := range rv.NumField() {
		f := rv.Type().Field(i)
		if !f.IsExported() || f.Type.Kind() != reflect.Pointer {
			continue
		}
		if rv.Field(i).IsNil() {
			t.Errorf("%s.%s missing from UI file", rv.Type().Name(), f.Name)
		}
	}
}

func readyState() *State {
	return &State{
		CLI: &poller.CLIStatus{Path: "/usr/bin/pangolin", Version: "0.18.1", HelperInstalled: true},
		Accounts: &poller.AccountStatus{Auth: pangolin.Auth{
			LoggedIn: true,
			Active:   pangolin.Account{UserID: "u1", Email: "me@example.com", Host: "https://app.pangolin.net", OrgID: "home", Active: true},
			Accounts: []pangolin.Account{
				{UserID: "u1", Email: "me@example.com", Host: "https://app.pangolin.net", OrgID: "home", Active: true},
				{UserID: "u2", Email: "work@example.com", Host: "https://pangolin.work"},
			},
		}, Config: pangolin.CLIConfig{OverrideDNS: true, UpstreamDNS: []string{"1.1.1.1", "9.9.9.9"}, MatchDomains: []string{"*.lan"}}},
		Server: &poller.ServerStatus{
			UserID: "u1", OrgID: "home",
			Info: server.Info{Version: "1.24.2", Build: "enterprise"},
			Orgs: []server.Org{{ID: "home", Name: "Home"}, {ID: "work", Name: "Work"}},
			ExitNodes: []server.ExitNode{
				{ResourceID: 7, NiceID: "exit-us", Name: "US Exit", SiteIDs: []int{2}},
				{ResourceID: 8, NiceID: "exit-eu", Name: "EU Exit", SiteIDs: []int{3}, SiteOnline: []bool{false}},
			},
		},
		Tunnel: &poller.TunnelStatus{},
	}
}

func onlineState() *State {
	st := readyState()
	st.Tunnel = &poller.TunnelStatus{Running: true, Status: &olm.Status{
		Connected: true, Registered: true, OrgID: "home", Version: "1.0.0",
		GatewayActive: true, GatewaySiteIDs: []int{2}, GatewayResource: 7,
		NetworkSettings: map[string]any{"tunnel_ip": "100.90.128.5/32"},
		Peers: map[int]olm.Peer{
			1: {SiteID: 1, Name: "Office", Connected: true, Endpoint: "203.0.113.1:51820", PeerIP: "100.89.0.1"},
			2: {SiteID: 2, Name: "Exit", Connected: true, IsRelay: true},
		},
	}}
	return st
}

func TestUIFilesComplete(t *testing.T) {
	a := testApp(t)
	assertFilled(t, a.win)
	assertFilled(t, NewOfflinePage(a))
	assertFilled(t, NewDevicePage(a))
	assertFilled(t, NewSitePage(a, olm.Peer{SiteID: 1}))
	assertFilled(t, NewPreferencesDialog(a))
}

func TestMainWindowOffline(t *testing.T) {
	a := testApp(t)
	st := readyState()
	a.state = *st
	a.win.Update(st)

	if got := a.win.stackPageNames(); !slices.Equal(got, []string{"offline"}) {
		t.Fatalf("pages %v", got)
	}
	if a.win.StatusSwitch.Active() || !a.win.StatusSwitch.Sensitive() {
		t.Fatal("switch should be off and usable")
	}
	off := a.win.pages["offline"].(*OfflinePage)
	if off.CLIGroup.Visible() || off.SetupGroup.Visible() || off.LoginGroup.Visible() || !off.ConnectGroup.Visible() {
		t.Fatal("ready state shows only Connect")
	}
	if a.win.AccountDropDown.Selected() != 0 || !a.win.AccountDropDown.Sensitive() {
		t.Fatalf("account dropdown selected=%d", a.win.AccountDropDown.Selected())
	}
}

func TestOfflinePageSteps(t *testing.T) {
	a := testApp(t)
	st := readyState()
	st.CLI.HelperInstalled = false
	st.Accounts.Auth = pangolin.Auth{}
	a.win.Update(st)
	off := a.win.pages["offline"].(*OfflinePage)
	if !off.SetupGroup.Visible() || !off.LoginGroup.Visible() || off.ConnectGroup.Visible() {
		t.Fatal("setup and login steps")
	}
	if a.win.StatusSwitch.Sensitive() {
		t.Fatal("switch disabled when logged out")
	}
}

func TestMainWindowOnline(t *testing.T) {
	a := testApp(t)
	a.win.Update(readyState())
	st := onlineState()
	a.win.Update(st)

	if got := a.win.stackPageNames(); !slices.Equal(got, []string{"device", "site:2", "site:1"}) {
		t.Fatalf("pages %v", got)
	}
	if !a.win.StatusSwitch.Active() || !a.win.StatusSwitch.State() {
		t.Fatal("switch on")
	}
	dev := a.win.pages["device"].(*DevicePage)
	if dev.AddressRow.Subtitle() != "100.90.128.5" || dev.ServerVersionRow.Subtitle() != "Pangolin 1.24.2 (Enterprise)" {
		t.Fatalf("device addr=%q server=%q", dev.AddressRow.Subtitle(), dev.ServerVersionRow.Subtitle())
	}
	if got := comboLabels(dev.choices.Org); !slices.Equal(got, []string{"Home", "Work"}) || dev.choices.Org.Row.Selected() != 0 || !dev.choices.Org.Row.Sensitive() {
		t.Fatalf("org choices %v selected %d", got, dev.choices.Org.Row.Selected())
	}
	if got := comboLabels(dev.choices.Exit); !slices.Equal(got, []string{"None", "US Exit", "EU Exit (offline)"}) || dev.choices.Exit.Row.Selected() != 1 || !dev.ExitGroup.Visible() {
		t.Fatalf("exit choices %v selected %d", got, dev.choices.Exit.Row.Selected())
	}
	site := a.win.pages["site:1"].(*SitePage)
	org := dev.choices.Org.Row
	if !org.UseSubtitle() || !org.HasCSSClass("property") || dev.ServerRow.SubtitleSelectable() || dev.AddressRow.SubtitleSelectable() {
		t.Fatal("device rows share the property style and are not selectable")
	}
	if !dev.copyServer.Enabled() || dev.ExitGroup.Description() != exitNodeHint {
		t.Fatalf("copy server=%v exit desc=%q", dev.copyServer.Enabled(), dev.ExitGroup.Description())
	}
	if site.Page.Title() != "Office" || site.ModeRow.Subtitle() != "Direct" || site.EndpointRow.Subtitle() != "203.0.113.1:51820" {
		t.Fatalf("site %q %q", site.Page.Title(), site.ModeRow.Subtitle())
	}
	if site.LatencyRow.Visible() {
		t.Fatal("empty latency hidden")
	}

	var sections []string
	for _, vp := range a.win.viewStackPages() {
		if vp.StartsSection() {
			sections = append(sections, vp.Name()+"="+vp.SectionTitle())
		}
	}
	if !slices.Equal(sections, []string{"site:2=Sites"}) {
		t.Fatalf("sections %v", sections)
	}

	a.win.Update(readyState())
	if got := a.win.stackPageNames(); !slices.Equal(got, []string{"offline"}) {
		t.Fatalf("after disconnect %v", got)
	}
}

func comboLabels(r *choiceRow) []string {
	labels := make([]string, r.model.NItems())
	for i := range labels {
		labels[i] = r.model.String(uint(i))
	}
	return labels
}

func menuLabels(m gio.MenuModeller) []string {
	model := gio.BaseMenuModel(m)
	var labels []string
	for i := range model.NItems() {
		if v := model.ItemAttributeValue(i, gio.MENU_ATTRIBUTE_LABEL, glib.NewVariantType("s")); v != nil {
			labels = append(labels, v.String())
		}
	}
	return labels
}

func TestMenuAndOfflineChoices(t *testing.T) {
	a := testApp(t)
	st := readyState()
	st.Accounts.Auth.Active.ExitNodeResourceID = 8
	a.state = *st
	a.syncActions()
	a.win.Update(st)

	if got := menuLabels(a.win.TunnelSection); !slices.Equal(got, []string{"_Organization", "_Exit Node"}) {
		t.Fatalf("menu %v", got)
	}
	exitMenu := a.win.TunnelSection.ItemLink(1, gio.MENU_LINK_SUBMENU)
	if got := menuLabels(exitMenu); !slices.Equal(got, []string{"None", "US Exit", "EU Exit (offline)"}) {
		t.Fatalf("exit submenu %v", got)
	}
	if got := a.actions["select-exit-node"].State().String(); got != "exit-eu" {
		t.Fatalf("exit action state %q", got)
	}
	if got := a.actions["select-org"].State().String(); got != "home" {
		t.Fatalf("org action state %q", got)
	}

	off := a.win.pages["offline"].(*OfflinePage)
	if !off.ChoicesGroup.Visible() || off.choices.Exit.Row.Selected() != 2 || !off.choices.Exit.Row.Visible() {
		t.Fatalf("offline exit selected %d", off.choices.Exit.Row.Selected())
	}

	// Without exit nodes the controls stay visible but greyed out, and
	// say why.
	st.Server.ExitNodes = nil
	st.Accounts.Auth.Active.ExitNodeResourceID = 0
	a.state = *st
	a.syncActions()
	a.win.Update(st)
	if got := menuLabels(a.win.TunnelSection); !slices.Equal(got, []string{"_Organization", "_Exit Node"}) {
		t.Fatalf("menu without exit nodes %v", got)
	}
	exitMenu = a.win.TunnelSection.ItemLink(1, gio.MENU_LINK_SUBMENU)
	if got := menuLabels(exitMenu); !slices.Equal(got, []string{"No exit nodes in this organization"}) {
		t.Fatalf("exit submenu %v", got)
	}
	if a.actions["select-exit-node"].Enabled() || a.actions["exit-nodes-unavailable"].Enabled() {
		t.Fatal("exit node actions disabled")
	}
	exit := off.choices.Exit.Row
	if !exit.Visible() || exit.Sensitive() || off.ExitGroup.Description() != "No exit nodes in this organization." {
		t.Fatalf("exit row visible=%v sensitive=%v %q", exit.Visible(), exit.Sensitive(), off.ExitGroup.Description())
	}

	st.Server.Info = server.Info{Version: "1.21.1"}
	a.win.Update(st)
	if off.ExitGroup.Description() != "Requires Pangolin 1.24 or newer on the server." {
		t.Fatalf("old server %q", off.ExitGroup.Description())
	}
}

func TestBusyKeepsChoice(t *testing.T) {
	a := testApp(t)
	st := readyState()
	a.win.Update(st)
	off := a.win.pages["offline"].(*OfflinePage)
	off.choices.Org.updating = true
	off.choices.Org.Row.SetSelected(1)
	off.choices.Org.updating = false

	st.Busy = "Switching organization…"
	a.win.Update(st)
	if off.choices.Org.Row.Selected() != 1 || off.choices.Org.Row.Sensitive() {
		t.Fatal("busy keeps the picked organization and locks the row")
	}
	st.Busy = ""
	a.win.Update(st)
	if off.choices.Org.Row.Selected() != 0 {
		t.Fatal("idle shows the real organization again")
	}
}

func TestBusyKeepsSwitch(t *testing.T) {
	a := testApp(t)
	st := readyState()
	a.win.Update(st)
	a.win.StatusSwitch.SetActive(true)
	st.Busy = "Connecting…"
	a.win.Update(st)
	if !a.win.StatusSwitch.Active() || a.win.StatusSwitch.Sensitive() || !a.win.WorkSpinner.Visible() {
		t.Fatal("busy should keep the requested position and lock the switch")
	}
}

func TestSyncActions(t *testing.T) {
	a := testApp(t)
	a.state = *readyState()
	a.syncActions()
	if !a.actions["connect"].Enabled() || a.actions["disconnect"].Enabled() || !a.actions["select-exit-node"].Enabled() || a.actions["setup"].Enabled() {
		t.Fatal("offline actions")
	}
	if a.actions["select-exit-node"].State().String() != "" {
		t.Fatal("no exit node saved")
	}
	a.state = *onlineState()
	a.syncActions()
	if a.actions["connect"].Enabled() || !a.actions["disconnect"].Enabled() || a.actions["reset-dns"].Enabled() {
		t.Fatal("online actions")
	}
	if a.actions["select-exit-node"].State().String() != "exit-us" {
		t.Fatalf("active exit node %q", a.actions["select-exit-node"].State().String())
	}
	a.state.Busy = "Disconnecting…"
	a.syncActions()
	if a.actions["disconnect"].Enabled() || a.actions["select-org"].Enabled() {
		t.Fatal("busy disables disconnect and switching")
	}
}

func TestPreferencesDialog(t *testing.T) {
	a := testApp(t)
	d := NewPreferencesDialog(a)
	st := readyState()
	d.Update(st)

	if d.EnableAliasesRow.Title() != Copy.EnableAliases || d.PreferLocalRow.Title() != Copy.PreferLocal || d.MTURow.Title() != Copy.MTU {
		t.Fatal("official wording")
	}
	if !d.EnableAliasesRow.Active() || d.TunnelDNSRow.Active() || !d.TunnelDNSRow.Sensitive() {
		t.Fatal("dns switches")
	}
	if d.PrimaryDNSRow.Text() != "1.1.1.1" || d.SecondaryDNSRow.Text() != "9.9.9.9" || d.MatchDomainsRow.Text() != "*.lan" {
		t.Fatalf("%q %q %q", d.PrimaryDNSRow.Text(), d.SecondaryDNSRow.Text(), d.MatchDomainsRow.Text())
	}
	if d.HelperButton.Visible() {
		t.Fatal("helper installed hides Set Up")
	}
	if len(d.accountRows) != 2 {
		t.Fatalf("account rows %d", len(d.accountRows))
	}

	st.Accounts.Config.OverrideDNS = false
	d.Update(st)
	if d.EnableAliasesRow.Active() || d.TunnelDNSRow.Sensitive() {
		t.Fatal("tunnel DNS requires aliases")
	}
}

func TestPrepareIconResolvesByName(t *testing.T) {
	gtkReady(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	path := prepareIcon()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, traygolin.IconPNG) {
		t.Fatal("cached icon differs from the embedded PNG")
	}
	if !gtk.IconThemeGetForDisplay(gdk.DisplayGetDefault()).HasIcon(metadata.AppID) {
		t.Fatalf("icon theme cannot find %s", metadata.AppID)
	}
	if gtk.WindowGetDefaultIconName() != metadata.AppID {
		t.Fatalf("default icon name = %q", gtk.WindowGetDefaultIconName())
	}
	if again := prepareIcon(); again != path {
		t.Fatalf("second call returned %q, want %q", again, path)
	}
}
