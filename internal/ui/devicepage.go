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
	_ "embed"
	"os"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/lucasssvaz/traygolin/internal/gutil"
	"github.com/lucasssvaz/traygolin/internal/tray"
)

//go:embed devicepage.ui
var devicePageXML string

// DevicePage shows this device's tunnel, account and exit node.
type DevicePage struct {
	app *App

	Page             *adw.StatusPage
	StatusRow        *adw.ActionRow
	AddressRow       *adw.ActionRow
	SitesRow         *adw.ActionRow
	ClientRow        *adw.ActionRow
	ServerVersionRow *adw.ActionRow
	AccountGroup     *adw.PreferencesGroup
	AccountRow       *adw.ActionRow
	ServerRow        *adw.ActionRow
	ExitGroup        *adw.PreferencesGroup

	choices     *ChoiceRows
	actions     *gio.SimpleActionGroup
	copyAddress *gio.SimpleAction
	copyServer  *gio.SimpleAction
	address     string
	server      string
}

func NewDevicePage(app *App) *DevicePage {
	page := DevicePage{app: app}
	gutil.FillFromUI(&page, devicePageXML)
	if host, err := os.Hostname(); err == nil {
		page.Page.SetDescription(host)
	}
	page.choices = NewChoiceRows(app)
	page.AccountGroup.Add(page.choices.Org.Row)
	page.ExitGroup.Add(page.choices.Exit.Row)

	page.actions = gio.NewSimpleActionGroup()
	page.copyAddress = gio.NewSimpleAction("copy-address", nil)
	page.copyAddress.ConnectActivate(func(*glib.Variant) { app.copyText(page.address, "Copied tunnel address") })
	page.actions.AddAction(page.copyAddress)
	page.copyServer = gio.NewSimpleAction("copy-server", nil)
	page.copyServer.ConnectActivate(func(*glib.Variant) { app.copyText(page.server, "Copied server address") })
	page.actions.AddAction(page.copyServer)

	return &page
}

func (page *DevicePage) Widget() gtk.Widgetter {
	return page.Page
}

func (page *DevicePage) Actions() gio.ActionGrouper {
	return page.actions
}

func (page *DevicePage) Bind(stackPage *adw.ViewStackPage) {
	stackPage.SetTitle("This Device")
	stackPage.SetIconName(page.Page.IconName())
}

func (page *DevicePage) Update(st *State) bool {
	if st.Tunnel == nil || !st.Tunnel.Running {
		return false
	}

	page.StatusRow.SetSubtitle(tray.StatusText(*st))

	status := st.Tunnel.Status
	page.address = ""
	if status != nil {
		page.address = status.TunnelIP()
	}
	rowText(page.AddressRow, page.address)
	page.copyAddress.SetEnabled(page.address != "")
	rowText(page.SitesRow, SitesText(st.Tunnel.Peers()))

	version := ""
	if st.CLI != nil {
		version = st.CLI.Version
	}
	rowText(page.ClientRow, ClientText(version, status))
	rowText(page.ServerVersionRow, tray.ServerInfo(*st).String())

	loggedIn := st.Accounts != nil && st.Accounts.Auth.LoggedIn
	page.server = ""
	if loggedIn {
		acc := st.Accounts.Auth.Active
		rowText(page.AccountRow, acc.Label())
		page.server = acc.Host
	} else {
		rowText(page.AccountRow, "Not logged in")
	}
	rowText(page.ServerRow, page.server)
	page.copyServer.SetEnabled(page.server != "")

	page.ExitGroup.SetDescription(page.choices.Update(st))
	page.ExitGroup.SetVisible(loggedIn)

	return true
}
