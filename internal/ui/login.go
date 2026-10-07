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
	"context"
	"strings"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/lucasssvaz/traygolin/internal/gutil"
	"github.com/lucasssvaz/traygolin/internal/metadata"
	"github.com/lucasssvaz/traygolin/internal/pangolin"
)

// LoginHost returns the server to log in to.
func LoginHost(cloud bool, custom string) string {
	if cloud {
		return metadata.CloudHost
	}
	custom = strings.TrimSpace(custom)
	if custom != "" && !strings.Contains(custom, "://") {
		custom = "https://" + custom
	}
	return custom
}

// showLogin asks for Pangolin Cloud or a self-hosted server, as the
// official clients do.
func (a *App) showLogin() {
	cloud := adw.NewActionRow()
	cloud.SetTitle("Pangolin Cloud")
	cloud.SetSubtitle(strings.TrimPrefix(metadata.CloudHost, "https://"))
	cloudCheck := gtk.NewCheckButton()
	cloudCheck.SetActive(true)
	cloud.AddPrefix(cloudCheck)
	cloud.SetActivatableWidget(cloudCheck)

	self := adw.NewActionRow()
	self.SetTitle("Self-hosted")
	self.SetSubtitle("Your own Pangolin server")
	selfCheck := gtk.NewCheckButton()
	selfCheck.SetGroup(cloudCheck)
	self.AddPrefix(selfCheck)
	self.SetActivatableWidget(selfCheck)

	host := adw.NewEntryRow()
	host.SetTitle("Server address")
	host.SetInputPurpose(gtk.InputPurposeURL)
	host.SetVisible(false)
	selfCheck.ConnectToggled(func() { host.SetVisible(selfCheck.Active()) })

	list := gtk.NewListBox()
	list.AddCSSClass("boxed-list")
	list.SetSelectionMode(gtk.SelectionNone)
	list.Append(cloud)
	list.Append(self)
	list.Append(host)

	dialog := adw.NewAlertDialog("Log In", "Choose where your Pangolin account is hosted.")
	dialog.SetExtraChild(list)
	dialog.AddResponse("cancel", "_Cancel")
	dialog.AddResponse("login", "_Continue")
	dialog.SetResponseAppearance("login", adw.ResponseSuggested)
	dialog.SetDefaultResponse("login")
	dialog.SetCloseResponse("cancel")
	dialog.ConnectResponse(func(response string) {
		if response != "login" {
			return
		}
		endpoint := LoginHost(cloudCheck.Active(), host.Text())
		if endpoint == "" {
			a.toast("Enter your Pangolin server address")
			return
		}
		a.runLogin(endpoint)
	})
	dialog.Present(gutil.PointerToWidgetter(a.window()))
}

// runLogin runs `pangolin login` and opens the device login page.
func (a *App) runLogin(host string) {
	urlRow := adw.NewActionRow()
	urlRow.SetTitle("Login page")
	urlRow.SetSubtitle("Waiting…")
	urlRow.SetSubtitleSelectable(true)
	urlRow.AddCSSClass("property")

	codeRow := adw.NewActionRow()
	codeRow.SetTitle("Code")
	codeRow.SetSubtitleSelectable(true)
	codeRow.AddCSSClass("property")
	codeRow.SetVisible(false)

	list := gtk.NewListBox()
	list.AddCSSClass("boxed-list")
	list.SetSelectionMode(gtk.SelectionNone)
	list.Append(urlRow)
	list.Append(codeRow)

	dialog := adw.NewAlertDialog("Logging In", "Finish logging in with your browser. If it did not open, visit the page below and enter the code.")
	dialog.SetExtraChild(list)
	dialog.AddResponse("cancel", "_Cancel")
	dialog.SetCloseResponse("cancel")

	ctx, cancel := context.WithTimeout(a.ctx, 5*time.Minute)
	dialog.ConnectResponse(func(string) { cancel() })
	dialog.Present(gutil.PointerToWidgetter(a.window()))

	var output strings.Builder
	opened := false
	go func() {
		defer cancel()
		_, err := a.cli.Login(ctx, host, func(ev pangolin.LoginEvent) {
			glib.IdleAdd(func() {
				if ev.Line != "" {
					output.WriteString(ev.Line)
					output.WriteByte('\n')
				}
				if ev.URL != "" {
					urlRow.SetSubtitle(ev.URL)
					if !opened {
						opened = true
						gtk.NewURILauncher(ev.URL).Launch(a.ctx, a.window(), nil)
					}
				}
				if ev.Code != "" {
					codeRow.SetSubtitle(ev.Code)
					codeRow.SetVisible(true)
				}
			})
		})
		canceled := ctx.Err() == context.Canceled
		a.pollNow(a.ctx)
		glib.IdleAdd(func() {
			dialog.ForceClose()
			switch {
			case canceled:
			case err != nil:
				Info{
					Heading: "Login Failed",
					Body:    glib.MarkupEscapeText(firstLine(err.Error())),
					Extra:   func() gtk.Widgetter { return codeLabel(strings.TrimSpace(output.String())) },
				}.Show(a, nil)
			default:
				a.toast("Logged in")
			}
		})
	}()
}
