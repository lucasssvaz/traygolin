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
	"context"
	_ "embed"
	"slices"
	"strconv"
	"strings"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/lucasssvaz/traygolin/internal/gutil"
	"github.com/lucasssvaz/traygolin/internal/pangolin"
)

//go:embed preferences.ui
var preferencesXML string

type PreferencesDialog struct {
	app *App

	PreferencesDialog *adw.PreferencesDialog

	StartAtLoginRow        *adw.SwitchRow
	ConnectAtStartRow      *adw.SwitchRow
	TrayIconRow            *adw.SwitchRow
	PollIntervalRow        *adw.SpinRow
	PollIntervalAdjustment *gtk.Adjustment
	HelperRow              *adw.ActionRow
	HelperButton           *gtk.Button

	EnableAliasesRow *adw.SwitchRow
	TunnelDNSRow     *adw.SwitchRow
	PrimaryDNSRow    *adw.EntryRow
	SecondaryDNSRow  *adw.EntryRow
	MatchDomainsRow  *adw.EntryRow
	ResetDNSRow      *adw.ActionRow

	PreferLocalRow  *adw.SwitchRow
	ExitPrecedeRow  *adw.SwitchRow
	HolepunchRow    *adw.SwitchRow
	DisableRelayRow *adw.SwitchRow
	MTURow          *adw.SpinRow
	MTUAdjustment   *gtk.Adjustment

	AccountsGroup *adw.PreferencesGroup

	updating    bool
	config      *pangolin.CLIConfig
	accounts    []pangolin.Account
	accountRows []gtk.Widgetter
}

func explain(row interface {
	SetTitle(string)
	SetSubtitle(string)
}, title, subtitle string) {
	row.SetTitle(title)
	row.SetSubtitle(subtitle)
}

func NewPreferencesDialog(app *App) *PreferencesDialog {
	d := PreferencesDialog{app: app}
	gutil.FillFromUI(&d, preferencesXML)

	explain(d.StartAtLoginRow, Copy.StartAtLogin, Copy.StartAtLoginDesc)
	explain(d.ConnectAtStartRow, Copy.ConnectAtStart, Copy.ConnectAtStartDesc)
	explain(d.TrayIconRow, Copy.TrayIcon, Copy.TrayIconDesc)
	explain(d.PollIntervalRow, Copy.PollTitle, Copy.PollDesc)
	explain(d.HelperRow, Copy.Helper, Copy.HelperDesc)
	explain(d.EnableAliasesRow, Copy.EnableAliases, Copy.EnableAliasesDesc)
	explain(d.TunnelDNSRow, Copy.TunnelDNS, Copy.TunnelDNSDesc)
	d.PrimaryDNSRow.SetTitle(Copy.PrimaryDNS)
	d.PrimaryDNSRow.SetTooltipText(Copy.PrimaryDNSDesc)
	d.SecondaryDNSRow.SetTitle(Copy.SecondaryDNS)
	d.SecondaryDNSRow.SetTooltipText(Copy.SecondaryDNSDesc)
	d.MatchDomainsRow.SetTitle(Copy.MatchDomains)
	d.MatchDomainsRow.SetTooltipText(Copy.MatchDomainsDesc)
	explain(d.ResetDNSRow, Copy.ResetDNS, Copy.ResetDNSDesc)
	explain(d.PreferLocalRow, Copy.PreferLocal, Copy.PreferLocalDesc)
	explain(d.ExitPrecedeRow, Copy.ExitPrecede, Copy.ExitPrecedeDesc)
	explain(d.HolepunchRow, Copy.Holepunch, Copy.HolepunchDesc)
	explain(d.DisableRelayRow, Copy.DisableRelay, Copy.DisableRelayDesc)
	explain(d.MTURow, Copy.MTU, Copy.MTUDesc)

	d.connectConfig()
	return &d
}

func (d *PreferencesDialog) setConfig(key, value string) {
	if d.updating {
		return
	}
	d.app.run(func(ctx context.Context) error {
		return d.app.cli.ConfigSet(ctx, key, value)
	}, "")
}

func (d *PreferencesDialog) connectConfig() {
	d.EnableAliasesRow.Connect("notify::active", func() {
		on := d.EnableAliasesRow.Active()
		d.TunnelDNSRow.SetSensitive(on)
		d.setConfig("up.override_dns", strconv.FormatBool(on))
	})
	d.TunnelDNSRow.Connect("notify::active", func() {
		d.setConfig("up.tunnel_dns", strconv.FormatBool(d.TunnelDNSRow.Active()))
	})
	applyUpstream := func() {
		d.setConfig("up.upstream_dns", JoinUpstream(d.PrimaryDNSRow.Text(), d.SecondaryDNSRow.Text()))
	}
	d.PrimaryDNSRow.ConnectApply(applyUpstream)
	d.SecondaryDNSRow.ConnectApply(applyUpstream)
	d.MatchDomainsRow.ConnectApply(func() {
		d.setConfig("up.match_domains_dns", strings.Join(pangolin.SplitCSV(d.MatchDomainsRow.Text()), ","))
	})
	d.PreferLocalRow.Connect("notify::active", func() {
		d.setConfig("up.prefer_local_routes", strconv.FormatBool(d.PreferLocalRow.Active()))
	})
	d.ExitPrecedeRow.Connect("notify::active", func() {
		d.setConfig("up.exit_node_takes_precedence", strconv.FormatBool(d.ExitPrecedeRow.Active()))
	})
}

// Update refreshes rows that mirror the CLI's files.
func (d *PreferencesDialog) Update(st *State) {
	helper := st.CLI != nil && st.CLI.HelperInstalled
	if helper {
		d.HelperRow.SetSubtitle("Set up. " + Copy.HelperDesc)
	} else {
		d.HelperRow.SetSubtitle("Not set up. " + Copy.HelperDesc)
	}
	d.HelperButton.SetVisible(!helper)

	if st.Accounts == nil {
		return
	}
	cfg := st.Accounts.Config
	if d.config == nil || !d.config.Equal(cfg) {
		d.config = &cfg
		d.updating = true
		d.EnableAliasesRow.SetActive(cfg.OverrideDNS)
		d.TunnelDNSRow.SetActive(cfg.TunnelDNS)
		d.TunnelDNSRow.SetSensitive(cfg.OverrideDNS)
		primary, secondary := SplitUpstream(cfg.UpstreamDNS)
		d.PrimaryDNSRow.SetText(primary)
		d.SecondaryDNSRow.SetText(secondary)
		d.MatchDomainsRow.SetText(strings.Join(cfg.MatchDomains, ", "))
		d.PreferLocalRow.SetActive(cfg.PreferLocalRoutes)
		d.ExitPrecedeRow.SetActive(cfg.ExitNodeTakesPrecedence)
		d.updating = false
	}
	d.updateAccounts(st.Accounts.Auth)
}

func (d *PreferencesDialog) updateAccounts(auth pangolin.Auth) {
	if d.accountRows != nil && slices.Equal(d.accounts, auth.Accounts) {
		return
	}
	d.accounts = slices.Clone(auth.Accounts)
	for _, row := range d.accountRows {
		d.AccountsGroup.Remove(row)
	}
	d.accountRows = d.accountRows[:0]

	if len(auth.Accounts) == 0 {
		row := adw.NewActionRow()
		row.SetTitle("No accounts")
		row.SetSubtitle("Add an account to connect.")
		d.AccountsGroup.Add(row)
		d.accountRows = append(d.accountRows, row)
		return
	}

	for _, acc := range auth.Accounts {
		acc := acc
		row := adw.NewActionRow()
		row.SetTitle(acc.Label())
		sub := acc.Host
		if acc.OrgID != "" {
			sub += " · " + acc.OrgID
		}
		row.SetSubtitle(sub)
		if acc.Active {
			check := gtk.NewImageFromIconName("object-select-symbolic")
			check.SetTooltipText("Active account")
			row.AddSuffix(check)
			logout := gtk.NewButtonWithLabel("Log Out")
			logout.SetVAlign(gtk.AlignCenter)
			logout.SetActionName("app.logout")
			row.AddSuffix(logout)
		} else {
			use := gtk.NewButtonWithLabel("Use")
			use.SetVAlign(gtk.AlignCenter)
			use.ConnectClicked(func() { d.app.selectAccount(acc) })
			row.AddSuffix(use)
		}
		d.AccountsGroup.Add(row)
		d.accountRows = append(d.accountRows, row)
	}
}
