// Copyright 2026 Lucas Saavedra Vaz
// Portions Copyright (c) 2025 DeedleFake, MIT License; see LICENSES/MIT-Trayscale.txt
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

// Package tray is the StatusNotifierItem icon and menu. Its structure and
// change tracking are adapted from Trayscale's internal/tray.
package tray

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unique"

	"deedles.dev/tray"
	"github.com/lucasssvaz/traygolin/internal/metadata"
	"github.com/lucasssvaz/traygolin/internal/pangolin"
	"github.com/lucasssvaz/traygolin/internal/poller"
	"github.com/lucasssvaz/traygolin/internal/server"
)

const iconSize = 32

var (
	statusHandle     = unique.Make("status")
	connToggleHandle = unique.Make("connToggle")
	accountsHandle   = unique.Make("accounts")
	orgHandle        = unique.Make("org")
	exitHandle       = unique.Make("exit")
	statusIconHandle = unique.Make("statusIcon")
	tooltipHandle    = unique.Make("tooltip")
)

// Icons are PNG images for each tunnel state.
type Icons struct {
	Inactive, Active, ExitNode []byte
}

type pixmaps struct {
	inactive, active, exitNode tray.Pixmap
}

func decode(data []byte) tray.Pixmap {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		img = image.NewRGBA(image.Rect(0, 0, iconSize, iconSize))
	}
	return tray.ToPixmap(resizeNearest(img, iconSize, iconSize))
}

func handler(f func()) tray.MenuItemProp {
	return tray.MenuItemHandler(tray.ClickedHandler(func(data any, timestamp uint32) error {
		if f != nil {
			f()
		}
		return nil
	}))
}

// Tray is the system tray icon. Callbacks are called from the D-Bus
// goroutine.
type Tray struct {
	Icons Icons

	OnShow          func()
	OnConnToggle    func()
	OnLogin         func()
	OnLogout        func()
	OnSelectAccount func(pangolin.Account)
	OnSelectOrg     func(orgID string)
	// OnSelectExit is called with an exit node's nice ID, or "" for None.
	OnSelectExit  func(niceID string)
	OnPreferences func()
	OnQuit        func()

	m      sync.Mutex
	item   *tray.Item
	prev   map[unique.Handle[string]][]any
	icons  pixmaps
	state  State
	accItm []*tray.MenuItem
	orgIt  []*tray.MenuItem
	exitIt []*tray.MenuItem

	statusItem     *tray.MenuItem
	connToggleItem *tray.MenuItem
	accountsItem   *tray.MenuItem
	orgItem        *tray.MenuItem
	exitItem       *tray.MenuItem
}

// State is the latest of each poller status kind.
type State struct {
	Tunnel   *poller.TunnelStatus
	Accounts *poller.AccountStatus
	Server   *poller.ServerStatus
	CLI      *poller.CLIStatus
	Busy     string
}

func (t *Tray) Start() error {
	t.m.Lock()
	defer t.m.Unlock()

	if t.item != nil {
		return nil
	}

	t.icons = pixmaps{
		inactive: decode(t.Icons.Inactive),
		active:   decode(t.Icons.Active),
		exitNode: decode(t.Icons.ExitNode),
	}

	item, err := tray.New(
		tray.ItemID(metadata.AppID),
		tray.ItemTitle(metadata.AppName),
		tray.ItemCategory(tray.ApplicationStatus),
		tray.ItemStatus(tray.Active),
		tray.ItemIconPixmap(&t.icons.inactive),
		tray.ItemIsMenu(true),
		tray.ItemHandler(tray.ActivateHandler(func(x, y int) error {
			if t.OnShow != nil {
				t.OnShow()
			}
			return nil
		})),
	)
	if err != nil {
		return err
	}
	t.item = item
	t.prev = make(map[unique.Handle[string]][]any)
	t.accItm, t.orgIt, t.exitIt = nil, nil, nil

	menu := item.Menu()

	t.statusItem, _ = menu.AddChild(tray.MenuItemLabel("Checking…"), tray.MenuItemEnabled(false))
	t.connToggleItem, _ = menu.AddChild(tray.MenuItemLabel("Connect"), handler(t.OnConnToggle))
	menu.AddChild(tray.MenuItemType(tray.Separator))
	t.accountsItem, _ = menu.AddChild(tray.MenuItemLabel("Account"))
	t.orgItem, _ = menu.AddChild(tray.MenuItemLabel("Organization"))
	t.exitItem, _ = menu.AddChild(tray.MenuItemLabel("Exit Node"), tray.MenuItemEnabled(false))
	menu.AddChild(tray.MenuItemType(tray.Separator))
	menu.AddChild(tray.MenuItemLabel("Show "+metadata.AppName), handler(t.OnShow))
	menu.AddChild(tray.MenuItemLabel("Preferences…"), handler(t.OnPreferences))
	menu.AddChild(tray.MenuItemType(tray.Separator))
	menu.AddChild(tray.MenuItemLabel("Quit"), handler(t.OnQuit))

	t.update()

	return nil
}

func (t *Tray) Close() error {
	if t == nil {
		return nil
	}

	t.m.Lock()
	defer t.m.Unlock()

	if t.item == nil {
		return nil
	}

	err := t.item.Close()
	t.item = nil
	t.prev = nil
	return err
}

// Update merges s into the tray's state and refreshes changed items.
func (t *Tray) Update(s poller.Status) {
	if t == nil {
		return
	}

	t.m.Lock()
	defer t.m.Unlock()

	switch s := s.(type) {
	case *poller.TunnelStatus:
		t.state.Tunnel = s
	case *poller.AccountStatus:
		t.state.Accounts = s
	case *poller.ServerStatus:
		t.state.Server = s
	case *poller.CLIStatus:
		t.state.CLI = s
	}
	t.update()
}

// SetBusy shows a transient state such as "Connecting…". An empty
// string clears it.
func (t *Tray) SetBusy(label string) {
	if t == nil {
		return
	}

	t.m.Lock()
	defer t.m.Unlock()

	t.state.Busy = label
	t.update()
}

func (t *Tray) dirty(key unique.Handle[string], vals ...any) bool {
	prev := t.prev[key]
	if slices.Equal(vals, prev) {
		return false
	}

	t.prev[key] = vals
	return true
}

func (t *Tray) update() {
	if t.item == nil {
		return
	}

	st := t.state
	label := StatusText(st)
	running := st.Tunnel != nil && st.Tunnel.Running
	loggedIn := st.Accounts != nil && st.Accounts.Auth.LoggedIn
	cliOK := st.CLI == nil || st.CLI.Found()

	t.updateStatusIcon(st)

	if t.dirty(tooltipHandle, label) {
		t.item.SetProps(tray.ItemToolTip("", nil, metadata.AppName, label))
	}

	if t.dirty(statusHandle, label) {
		t.statusItem.SetProps(tray.MenuItemLabel(label))
	}

	toggle := ConnToggleText(running)
	enabled := cliOK && st.Busy == "" && (running || loggedIn)
	if t.dirty(connToggleHandle, toggle, enabled) {
		t.connToggleItem.SetProps(tray.MenuItemLabel(toggle), tray.MenuItemEnabled(enabled))
	}

	t.updateAccounts(st)
	t.updateOrgs(st, loggedIn && st.Busy == "")
	t.updateExit(st, loggedIn && st.Busy == "")
}

// replaceChildren removes the previous children of a submenu and returns
// a function that adds new ones to it.
func replaceChildren(parent *tray.MenuItem, items *[]*tray.MenuItem) func(...tray.MenuItemProp) {
	for _, it := range *items {
		it.Remove()
	}
	*items = (*items)[:0]
	return func(props ...tray.MenuItemProp) {
		if it, err := parent.AddChild(props...); err == nil {
			*items = append(*items, it)
		}
	}
}

func (t *Tray) updateOrgs(st State, enabled bool) {
	current := CurrentOrg(st)
	var orgs []server.Org
	if st.Server != nil {
		orgs = st.Server.Orgs
	}
	key := []any{current, OrgLabel(st), enabled}
	for _, o := range orgs {
		key = append(key, o)
	}
	if !t.dirty(orgHandle, key...) {
		return
	}

	t.orgItem.SetProps(tray.MenuItemLabel("Organization: "+OrgLabel(st)), tray.MenuItemEnabled(enabled))
	add := replaceChildren(t.orgItem, &t.orgIt)
	if len(orgs) == 0 {
		add(tray.MenuItemLabel("No organizations available"), tray.MenuItemEnabled(false))
		return
	}
	for _, o := range orgs {
		id := o.ID
		add(
			tray.MenuItemLabel(o.Label()),
			tray.MenuItemToggleType(tray.Radio),
			tray.MenuItemToggleState(toggleState(id == current)),
			handler(func() {
				if id != current && t.OnSelectOrg != nil {
					t.OnSelectOrg(id)
				}
			}),
		)
	}
}

func (t *Tray) updateExit(st State, enabled bool) {
	current := CurrentExitNode(st)
	label := ExitNodeLabel(st)
	unavailable := ExitNodesUnavailable(st)
	if unavailable != "" {
		label = unavailable
		enabled = false
	}
	var nodes []server.ExitNode
	if st.Server != nil {
		nodes = st.Server.ExitNodes
	}
	key := []any{current, label, enabled}
	for _, e := range nodes {
		key = append(key, e.ResourceID, e.NiceID, e.Label(), e.Online())
	}
	if !t.dirty(exitHandle, key...) {
		return
	}

	t.exitItem.SetProps(
		tray.MenuItemLabel("Exit Node: "+label),
		tray.MenuItemEnabled(enabled),
	)
	add := replaceChildren(t.exitItem, &t.exitIt)
	add(
		tray.MenuItemLabel("None"),
		tray.MenuItemToggleType(tray.Radio),
		tray.MenuItemToggleState(toggleState(current == 0)),
		handler(func() {
			if current != 0 && t.OnSelectExit != nil {
				t.OnSelectExit("")
			}
		}),
	)
	for _, e := range nodes {
		e := e
		add(
			tray.MenuItemLabel(ExitNodeOptionLabel(e)),
			tray.MenuItemToggleType(tray.Radio),
			tray.MenuItemToggleState(toggleState(e.ResourceID == current)),
			handler(func() {
				if e.ResourceID != current && t.OnSelectExit != nil {
					t.OnSelectExit(e.NiceID)
				}
			}),
		)
	}
}

func (t *Tray) updateStatusIcon(st State) {
	icon := &t.icons.inactive
	switch {
	case st.Tunnel.ExitNodeActive():
		icon = &t.icons.exitNode
	case st.Tunnel.Online():
		icon = &t.icons.active
	}
	if !t.dirty(statusIconHandle, icon) {
		return
	}

	t.item.SetProps(tray.ItemIconPixmap(icon))
}

func (t *Tray) updateAccounts(st State) {
	var auth pangolin.Auth
	if st.Accounts != nil {
		auth = st.Accounts.Auth
	}
	key := make([]any, 0, len(auth.Accounts)+1)
	key = append(key, auth.LoggedIn)
	for _, acc := range auth.Accounts {
		key = append(key, acc)
	}
	if !t.dirty(accountsHandle, key...) {
		return
	}

	title := "Account: Not logged in"
	if auth.LoggedIn {
		title = "Account: " + auth.Active.Label()
	}
	t.accountsItem.SetProps(tray.MenuItemLabel(title))

	for _, it := range t.accItm {
		it.Remove()
	}
	t.accItm = t.accItm[:0]
	add := func(props ...tray.MenuItemProp) {
		it, err := t.accountsItem.AddChild(props...)
		if err == nil {
			t.accItm = append(t.accItm, it)
		}
	}
	for _, acc := range auth.Accounts {
		acc := acc
		add(
			tray.MenuItemLabel(acc.Label()),
			tray.MenuItemToggleType(tray.Radio),
			tray.MenuItemToggleState(toggleState(acc.Active)),
			handler(func() {
				if t.OnSelectAccount != nil && !acc.Active {
					t.OnSelectAccount(acc)
				}
			}),
		)
	}
	if len(auth.Accounts) > 0 {
		add(tray.MenuItemType(tray.Separator))
	}
	add(tray.MenuItemLabel("Add Account…"), handler(t.OnLogin))
	if auth.LoggedIn {
		add(tray.MenuItemLabel("Log Out"), handler(t.OnLogout))
	}
}

func toggleState(on bool) tray.MenuToggleState {
	if on {
		return tray.On
	}
	return tray.Off
}

// CurrentOrg is the organization ID in use, preferring the tunnel's.
func CurrentOrg(st State) string {
	if st.Tunnel != nil && st.Tunnel.Status != nil && st.Tunnel.Status.OrgID != "" {
		return st.Tunnel.Status.OrgID
	}
	if st.Accounts != nil {
		return st.Accounts.Auth.Active.OrgID
	}
	return ""
}

// OrgLabel names the organization in use.
func OrgLabel(st State) string {
	id := CurrentOrg(st)
	if id == "" {
		return "None"
	}
	if o, ok := st.Server.Org(id); ok {
		return o.Label()
	}
	return id
}

// CurrentExitNode is the resource ID of the exit node in use: the
// tunnel's while it runs, otherwise the one saved for the next
// connection. It is 0 for none.
func CurrentExitNode(st State) int {
	if st.Tunnel != nil && st.Tunnel.Running && st.Tunnel.Status != nil {
		if st.Tunnel.Status.GatewayActive {
			return st.Tunnel.Status.GatewayResource
		}
		return 0
	}
	if st.Accounts != nil {
		return st.Accounts.Auth.Active.ExitNodeResourceID
	}
	return 0
}

// ExitNodesAvailable reports whether there is an exit node to choose or
// one in use.
func ExitNodesAvailable(st State) bool {
	return (st.Server != nil && len(st.Server.ExitNodes) > 0) || CurrentExitNode(st) != 0
}

// ExitNodesUnavailable explains why exit nodes cannot be chosen, or
// returns "" when they can.
func ExitNodesUnavailable(st State) string {
	if ExitNodesAvailable(st) {
		return ""
	}
	if info := ServerInfo(st); !server.SupportsExitNodes(info.Version) {
		return "Requires Pangolin " + strings.TrimSuffix(server.ExitNodesVersion, ".0") + " or newer on the server"
	}
	return "No exit nodes in this organization"
}

// ServerInfo is the active account's server version, live when known and
// otherwise as saved at login.
func ServerInfo(st State) server.Info {
	if st.Server != nil && st.Server.Info.Version != "" {
		return st.Server.Info
	}
	if st.Accounts != nil {
		return st.Accounts.Auth.Active.Server
	}
	return server.Info{}
}

// ExitNodeLabel names the exit node in use, or "None".
func ExitNodeLabel(st State) string {
	id := CurrentExitNode(st)
	if id == 0 {
		return "None"
	}
	if e, ok := st.Server.ExitNode(id); ok {
		return e.Label()
	}
	if sites := ExitSites(st.Tunnel); sites != "" && sites != "Active" {
		return sites
	}
	return "Exit node " + strconv.Itoa(id)
}

// ExitNodeOptionLabel is how an exit node is listed in menus.
func ExitNodeOptionLabel(e server.ExitNode) string {
	if !e.Online() {
		return e.Label() + " (offline)"
	}
	return e.Label()
}

// StatusText is the one-line state shown in the tray and window.
func StatusText(st State) string {
	switch {
	case st.Busy != "":
		return st.Busy
	case st.CLI != nil && !st.CLI.Found():
		return "Pangolin CLI not installed"
	case st.Tunnel.Unreadable():
		return "Connected (status unavailable)"
	case st.Tunnel.Online():
		n := 0
		for _, p := range st.Tunnel.Peers() {
			if p.Connected {
				n++
			}
		}
		return fmt.Sprintf("Connected · %d of %d sites", n, len(st.Tunnel.Peers()))
	case st.Tunnel.Connecting():
		return "Connecting…"
	case st.Accounts != nil && !st.Accounts.Auth.LoggedIn:
		return "Not logged in"
	}
	return "Disconnected"
}

// ConnToggleText is the label for the connect toggle.
func ConnToggleText(running bool) string {
	if running {
		return "Disconnect"
	}
	return "Connect"
}

// ExitSites names the sites currently used as the exit node.
func ExitSites(s *poller.TunnelStatus) string {
	if !s.ExitNodeActive() {
		return ""
	}
	var names []string
	for _, p := range s.Peers() {
		if s.Status.IsGateway(p.SiteID) {
			names = append(names, p.Name)
		}
	}
	if len(names) == 0 {
		return "Active"
	}
	return strings.Join(names, ", ")
}

func resizeNearest(src image.Image, w, h int) image.Image {
	if src == nil || w <= 0 || h <= 0 {
		return src
	}
	b := src.Bounds()
	if b.Dx() == w && b.Dy() == h {
		return src
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	sw, sh := b.Dx(), b.Dy()
	for y := range h {
		sy := b.Min.Y + y*sh/h
		for x := range w {
			sx := b.Min.X + x*sw/w
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}
