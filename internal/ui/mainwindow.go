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

package ui

import (
	_ "embed"
	"slices"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/lucasssvaz/traygolin/internal/gutil"
	"github.com/lucasssvaz/traygolin/internal/olm"
	"github.com/lucasssvaz/traygolin/internal/pangolin"
	"github.com/lucasssvaz/traygolin/internal/tray"
)

var (
	//go:embed mainwindow.ui
	mainWindowXML string

	//go:embed menu.ui
	menuXML string
)

type MainWindow struct {
	app *App

	MainWindow      *adw.ApplicationWindow
	ToastOverlay    *adw.ToastOverlay
	SplitView       *adw.NavigationSplitView
	StatusSwitch    *gtk.Switch
	MainMenuButton  *gtk.MenuButton
	PagesSidebar    *adw.ViewSwitcherSidebar
	PagesStack      *adw.ViewStack
	WorkSpinner     *adw.Spinner
	AccountDropDown *gtk.DropDown
	PageMenuButton  *gtk.MenuButton
	TunnelSection   *gio.Menu

	pages map[string]Page
	menu  []Choice

	accounts         []pangolin.Account
	accountModel     *gtk.StringList
	updatingAccounts bool
}

func NewMainWindow(app *App) *MainWindow {
	win := MainWindow{
		app:   app,
		pages: make(map[string]Page),
	}
	gutil.FillFromUI(&win, menuXML, mainWindowXML)

	win.MainWindow.SetApplication(&app.app.Application)

	win.PagesStack.NotifyProperty("visible-child-name", func() {
		page := win.pages[win.PagesStack.VisibleChildName()]

		var actions gio.ActionGrouper
		if page != nil {
			actions = page.Actions()
		}
		win.MainWindow.InsertActionGroup("page", actions)
		win.PageMenuButton.SetVisible(actions != nil)
	})
	win.PageMenuButton.SetVisible(false)

	win.accountModel = gtk.NewStringList(nil)
	win.AccountDropDown.SetModel(win.accountModel)

	win.StatusSwitch.ConnectStateSet(func(s bool) bool {
		if s == win.StatusSwitch.State() {
			return false
		}

		if s {
			app.startTunnel()
		} else {
			app.stopTunnel()
		}
		return true
	})

	win.AccountDropDown.NotifyProperty("selected", func() {
		if win.updatingAccounts {
			return
		}
		i := int(win.AccountDropDown.Selected())
		if i < 0 || i >= len(win.accounts) {
			return
		}
		acc := win.accounts[i]
		if acc.Active {
			return
		}
		app.selectAccount(acc)
	})

	contentVariant := glib.NewVariantString("content")
	win.PagesSidebar.ConnectActivated(func() {
		win.SplitView.ActivateAction("navigation.push", contentVariant)
	})

	return &win
}

func (win *MainWindow) addPage(name string, page Page) {
	win.pages[name] = page
	vp := win.PagesStack.AddNamed(page.Widget(), name)
	page.Bind(vp)
}

func (win *MainWindow) viewStackPages() []*adw.ViewStackPage {
	model := win.PagesStack.Pages()
	pages := make([]*adw.ViewStackPage, 0, model.NItems())
	for i := range model.NItems() {
		obj := model.Item(i)
		if obj == nil {
			continue
		}
		vp, ok := obj.Cast().(*adw.ViewStackPage)
		if !ok {
			continue
		}
		pages = append(pages, vp)
	}
	return pages
}

func (win *MainWindow) removePage(name string, page Page) {
	reselect := win.PagesStack.VisibleChildName() == name

	delete(win.pages, name)
	win.PagesStack.Remove(page.Widget())

	if !reselect {
		return
	}
	if remaining := win.viewStackPages(); len(remaining) > 0 {
		win.PagesStack.SetVisibleChildName(remaining[0].Name())
	}
}

func (win *MainWindow) stackPageNames() []string {
	vps := win.viewStackPages()
	names := make([]string, 0, len(vps))
	for _, vp := range vps {
		names = append(names, vp.Name())
	}
	return names
}

// Update refreshes the window from the app's merged state.
func (win *MainWindow) Update(st *State) {
	running := st.Tunnel != nil && st.Tunnel.Running
	if st.Busy == "" {
		win.StatusSwitch.SetState(running)
		win.StatusSwitch.SetActive(running)
	}
	cliOK := st.CLI == nil || st.CLI.Found()
	loggedIn := st.Accounts != nil && st.Accounts.Auth.LoggedIn
	win.StatusSwitch.SetSensitive(cliOK && st.Busy == "" && (running || loggedIn))
	win.WorkSpinner.SetVisible(st.Busy != "")

	if st.Accounts != nil {
		win.updateAccounts(st.Accounts.Auth)
	}
	win.updateMenu(st)
	win.updatePages(st)
}

// updateMenu rebuilds the main menu's Organization and Exit Node
// submenus. Their radio state comes from the app.select-* actions.
func (win *MainWindow) updateMenu(st *State) {
	loggedIn := st.Accounts != nil && st.Accounts.Auth.LoggedIn
	orgs, _ := OrgChoices(st)
	exits, _ := ExitChoices(st)
	exitAction := "app.select-exit-node"
	if reason := tray.ExitNodesUnavailable(*st); reason != "" {
		exits = []Choice{{ID: "unavailable", Label: reason}}
		exitAction = "app.exit-nodes-unavailable"
	}
	if !loggedIn {
		orgs, exits = nil, nil
	}

	key := append(append([]Choice{{ID: "org"}}, orgs...), append([]Choice{{ID: exitAction}}, exits...)...)
	if slices.Equal(win.menu, key) {
		return
	}
	win.menu = key

	submenu := func(action string, choices []Choice) *gio.Menu {
		m := gio.NewMenu()
		for _, c := range choices {
			if c.ID == "" && c.Label != "None" {
				continue
			}
			item := gio.NewMenuItem(c.Label, "")
			item.SetActionAndTargetValue(action, glib.NewVariantString(c.ID))
			m.AppendItem(item)
		}
		return m
	}
	win.TunnelSection.RemoveAll()
	if len(orgs) > 0 {
		win.TunnelSection.AppendSubmenu("_Organization", submenu("app.select-org", orgs))
	}
	if len(exits) > 0 {
		win.TunnelSection.AppendSubmenu("_Exit Node", submenu(exitAction, exits))
	}
}

func (win *MainWindow) updatePages(st *State) {
	running := st.Tunnel != nil && st.Tunnel.Running
	if !running {
		if _, ok := win.pages["offline"]; !ok {
			win.addPage("offline", NewOfflinePage(win.app))
		}
	} else {
		if _, ok := win.pages["device"]; !ok {
			win.addPage("device", NewDevicePage(win.app))
		}
		// Site pages only show while online, so making them earlier would
		// build and drop them again on every refresh while connecting.
		var peers []olm.Peer
		if st.Tunnel.Online() {
			peers = st.Tunnel.Peers()
		}
		for _, peer := range peers {
			name := sitePageName(peer.SiteID)
			if _, ok := win.pages[name]; !ok {
				win.addPage(name, NewSitePage(win.app, peer))
			}
		}
	}

	var remove []string
	for name, page := range win.pages {
		if !page.Update(st) {
			remove = append(remove, name)
		}
	}
	for _, name := range remove {
		win.removePage(name, win.pages[name])
	}

	layout := SidebarLayout(st, win.pages)
	if !slices.Equal(win.stackPageNames(), layout) {
		win.restack(layout)
	}
	win.applySections(layout)
}

// SidebarLayout is the page order: offline, or this device then sites
// by name.
func SidebarLayout[P any](st *State, pages map[string]P) []string {
	if st.Tunnel == nil || !st.Tunnel.Running {
		if _, ok := pages["offline"]; ok {
			return []string{"offline"}
		}
		return nil
	}
	var layout []string
	if _, ok := pages["device"]; ok {
		layout = append(layout, "device")
	}
	for _, peer := range st.Tunnel.Peers() {
		name := sitePageName(peer.SiteID)
		if _, ok := pages[name]; ok {
			layout = append(layout, name)
		}
	}
	return layout
}

func (win *MainWindow) restack(order []string) {
	visible := win.PagesStack.VisibleChildName()
	for _, vp := range win.viewStackPages() {
		if page := win.pages[vp.Name()]; page != nil {
			win.PagesStack.Remove(page.Widget())
		}
	}
	for _, name := range order {
		if page := win.pages[name]; page != nil {
			win.addPage(name, page)
		}
	}
	for _, vp := range win.viewStackPages() {
		if vp.Name() == visible {
			win.PagesStack.SetVisibleChildName(visible)
			return
		}
	}
	if len(order) > 0 {
		win.PagesStack.SetVisibleChildName(order[0])
	}
}

func (win *MainWindow) applySections(layout []string) {
	first := ""
	for _, name := range layout {
		if name != "device" && name != "offline" {
			first = name
			break
		}
	}
	for _, vp := range win.viewStackPages() {
		starts := vp.Name() == first
		vp.SetStartsSection(starts)
		if starts {
			vp.SetSectionTitle("Sites")
		} else {
			vp.SetSectionTitle("")
		}
	}
}

func (win *MainWindow) updateAccounts(auth pangolin.Auth) {
	if slices.Equal(win.accounts, auth.Accounts) {
		return
	}
	win.updatingAccounts = true
	defer func() { win.updatingAccounts = false }()

	win.accounts = slices.Clone(auth.Accounts)
	labels := make([]string, 0, len(auth.Accounts))
	selected := uint(gtk.InvalidListPosition)
	for i, acc := range auth.Accounts {
		labels = append(labels, acc.Label())
		if acc.Active {
			selected = uint(i)
		}
	}
	if len(labels) == 0 {
		labels = append(labels, "Not logged in")
		selected = 0
	}
	win.accountModel.Splice(0, win.accountModel.NItems(), labels)
	win.AccountDropDown.SetSelected(selected)
	win.AccountDropDown.SetSensitive(len(auth.Accounts) > 1)
}

func (win *MainWindow) Toast(msg string) *adw.Toast {
	toast := adw.NewToast(msg)
	toast.SetTimeout(3)
	win.ToastOverlay.AddToast(toast)
	return toast
}
