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

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/lucasssvaz/traygolin/internal/gutil"
	"github.com/lucasssvaz/traygolin/internal/metadata"
	"github.com/lucasssvaz/traygolin/internal/tray"
)

//go:embed offlinepage.ui
var offlinePageXML string

// OfflinePage is shown while the tunnel is not running. It also walks
// through what is missing before Connect can work.
type OfflinePage struct {
	app *App

	Page          *adw.StatusPage
	CLIGroup      *adw.PreferencesGroup
	CLIInstallRow *adw.ActionRow
	SetupGroup    *adw.PreferencesGroup
	LoginGroup    *adw.PreferencesGroup
	ChoicesGroup  *adw.PreferencesGroup
	ExitGroup     *adw.PreferencesGroup
	ConnectGroup  *adw.PreferencesGroup
	ConnectRow    *adw.ButtonRow

	choices *ChoiceRows
}

func NewOfflinePage(app *App) *OfflinePage {
	page := OfflinePage{app: app}
	gutil.FillFromUI(&page, offlinePageXML)
	page.CLIInstallRow.SetSubtitle(metadata.CLIInstall)
	page.choices = NewChoiceRows(app)
	page.ChoicesGroup.Add(page.choices.Org.Row)
	page.ExitGroup.Add(page.choices.Exit.Row)
	return &page
}

func (page *OfflinePage) Widget() gtk.Widgetter {
	return page.Page
}

func (page *OfflinePage) Actions() gio.ActionGrouper {
	return nil
}

func (page *OfflinePage) Bind(stackPage *adw.ViewStackPage) {
	stackPage.SetTitle("Not Connected")
	stackPage.SetIconName(page.Page.IconName())
}

// OfflineNeeds reports which setup steps are still missing.
func OfflineNeeds(st *State) (cli, setup, login bool) {
	cli = st.CLI != nil && !st.CLI.Found()
	setup = !cli && st.CLI != nil && !st.CLI.HelperInstalled
	login = !cli && st.Accounts != nil && !st.Accounts.Auth.LoggedIn
	return cli, setup, login
}

func (page *OfflinePage) Update(st *State) bool {
	if st.Tunnel != nil && st.Tunnel.Running {
		return false
	}

	cli, setup, login := OfflineNeeds(st)
	page.CLIGroup.SetVisible(cli)
	page.SetupGroup.SetVisible(setup)
	page.LoginGroup.SetVisible(login)
	ready := !cli && !setup && !login
	page.ConnectGroup.SetVisible(ready)
	page.ExitGroup.SetDescription(page.choices.Update(st))
	page.ChoicesGroup.SetVisible(!cli && !login)
	page.ExitGroup.SetVisible(!cli && !login)
	title := "Connect"
	if st.Busy != "" {
		title = st.Busy
	}
	page.ConnectRow.SetSensitive(st.Busy == "")
	page.ConnectRow.SetTitle(title)

	desc := tray.StatusText(*st)
	switch {
	case cli:
		desc = "Install the Pangolin CLI to continue"
	case setup:
		desc = "One-time setup is needed before connecting"
	case login:
		desc = "Log in to connect to your organization"
	}
	page.Page.SetDescription(desc)
	return true
}
