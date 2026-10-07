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
	"log/slog"
	"os"
	"path/filepath"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/lucasssvaz/traygolin/internal/autostart"
	"github.com/lucasssvaz/traygolin/internal/metadata"
	"github.com/lucasssvaz/traygolin/internal/pangolin"
)

func (a *App) initSettings(ctx context.Context) {
	if a.settings == nil {
		a.runSettings(ctx)
		return
	}

	a.settings.ConnectChanged(func(key string) {
		switch key {
		case "tray-icon":
			glib.IdleAdd(func() {
				if a.settings.Boolean("tray-icon") {
					a.initTray(ctx)
					return
				}
				a.tray.Close()
				a.tray = nil
			})

		case "poll-interval":
			if a.poller != nil && a.pollOverride == 0 {
				go func() { a.poller.SetInterval() <- a.getInterval() }()
			}

		case "start-at-login":
			a.syncAutostart()
		}
	})

	a.syncAutostart()
	a.runSettings(ctx)
}

func (a *App) runSettings(ctx context.Context) {
	if a.settings == nil || a.settings.Boolean("tray-icon") {
		glib.IdleAdd(func() {
			a.initTray(ctx)
		})
	}
}

func (a *App) syncAutostart() {
	want := a.settings.Boolean("start-at-login")
	if !want && !autostart.Enabled() {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		slog.Error("find executable", "err", err)
		return
	}
	if err := autostart.Set(want, exe); err != nil {
		slog.Error("update autostart", "err", err)
		glib.IdleAdd(func() { a.toast("Could not update Start at Login: " + err.Error()) })
	}
}

func (a *App) showPreferences() {
	if a.settings == nil {
		a.toast("Settings schema not found")
		return
	}

	dialog := NewPreferencesDialog(a)
	bind := func(key string, obj *coreglib.Object, prop string) {
		a.settings.Bind(key, obj, prop, gio.SettingsBindDefault)
	}
	bind("start-at-login", dialog.StartAtLoginRow.Object, "active")
	bind("connect-at-start", dialog.ConnectAtStartRow.Object, "active")
	bind("tray-icon", dialog.TrayIconRow.Object, "active")
	bind("holepunch", dialog.HolepunchRow.Object, "active")
	bind("disable-relay", dialog.DisableRelayRow.Object, "active")
	bind("mtu", dialog.MTUAdjustment.Object, "value")
	if a.pollOverride > 0 {
		dialog.PollIntervalAdjustment.SetValue(a.pollOverride.Seconds())
		dialog.PollIntervalRow.SetSensitive(false)
		dialog.PollIntervalRow.SetSubtitle("Set by --poll-interval for this session")
	} else {
		bind("poll-interval", dialog.PollIntervalAdjustment.Object, "value")
	}

	dialog.Update(&a.state)
	a.prefs = dialog
	dialog.PreferencesDialog.ConnectClosed(func() {
		if a.prefs == dialog {
			a.prefs = nil
		}
	})
	dialog.PreferencesDialog.Present(a.window())
}

func (a *App) logPath() string {
	if a.state.Accounts != nil && a.state.Accounts.Config.LogFile != "" {
		return a.state.Accounts.Config.LogFile
	}
	return pangolin.DefaultLogPath()
}

// showLogs shows the end of the Pangolin client log.
func (a *App) showLogs() {
	path := a.logPath()

	view := gtk.NewTextView()
	view.SetEditable(false)
	view.SetCursorVisible(false)
	view.SetMonospace(true)
	view.SetWrapMode(gtk.WrapWordChar)
	view.SetTopMargin(12)
	view.SetBottomMargin(12)
	view.SetLeftMargin(12)
	view.SetRightMargin(12)

	scroller := gtk.NewScrolledWindow()
	scroller.SetVExpand(true)
	scroller.SetChild(view)

	load := func() {
		text, err := pangolin.TailLog(path, 256*1024)
		if err != nil {
			text = "Could not read " + path + ": " + err.Error()
		} else if text == "" {
			text = "The log is empty."
		}
		buf := view.Buffer()
		buf.SetText(text)
		glib.IdleAdd(func() {
			adj := scroller.VAdjustment()
			adj.SetValue(adj.Upper())
		})
	}

	refresh := gtk.NewButtonFromIconName("view-refresh-symbolic")
	refresh.SetTooltipText("Refresh")
	refresh.ConnectClicked(load)

	open := gtk.NewButtonFromIconName("folder-open-symbolic")
	open.SetTooltipText("Open Log Folder")
	open.ConnectClicked(func() {
		gtk.NewURILauncher("file://"+filepath.Dir(path)).Launch(a.ctx, a.window(), nil)
	})

	header := adw.NewHeaderBar()
	header.PackStart(refresh)
	header.PackStart(open)
	title := adw.NewWindowTitle("Logs", path)
	header.SetTitleWidget(title)

	toolbar := adw.NewToolbarView()
	toolbar.AddTopBar(header)
	toolbar.SetContent(scroller)

	dialog := adw.NewDialog()
	dialog.SetTitle("Logs")
	dialog.SetContentWidth(760)
	dialog.SetContentHeight(520)
	dialog.SetChild(toolbar)
	load()
	dialog.Present(a.window())
}

// showAbout shows the app's about dialog.
func (a *App) showAbout() {
	dialog := adw.NewAboutDialog()
	dialog.SetApplicationName(metadata.AppName)
	dialog.SetApplicationIcon(metadata.AppID)
	dialog.SetVersion(metadata.Version)
	dialog.SetDeveloperName("Lucas Saavedra Vaz")
	dialog.SetCopyright("Copyright 2026 Lucas Saavedra Vaz")
	dialog.SetLicenseType(gtk.LicenseApache20)
	dialog.SetWebsite(metadata.Website)
	dialog.SetIssueURL(metadata.Website + "/issues")
	dialog.SetComments("A tray app for the Pangolin VPN client.\n\n" + Copy.Disclaimer)
	dialog.AddAcknowledgementSection("Based on", []string{"Trayscale by DeedleFake https://github.com/DeedleFake/trayscale"})
	dialog.AddLegalSection("Trayscale", "Copyright (c) 2025 DeedleFake", gtk.LicenseMITX11, "")
	dialog.AddLegalSection("Trademarks", "", gtk.LicenseCustom, Copy.LogoWhy)
	dialog.Present(a.window())
}
