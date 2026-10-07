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
	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/lucasssvaz/traygolin/internal/gutil"
)

func (a *App) window() *gtk.Window {
	if a == nil {
		return nil
	}
	if a.win == nil {
		return nil
	}

	return &a.win.MainWindow.Window
}

type Confirmation struct {
	Heading     string
	Body        string
	Accept      string
	Reject      string
	Destructive bool
}

func (d Confirmation) Show(a *App, res func(bool)) {
	dialog := adw.NewAlertDialog(d.Heading, d.Body)
	dialog.AddResponse("reject", d.Reject)
	dialog.SetCloseResponse("reject")
	dialog.AddResponse("accept", d.Accept)
	appearance := adw.ResponseSuggested
	if d.Destructive {
		appearance = adw.ResponseDestructive
	}
	dialog.SetResponseAppearance("accept", appearance)
	dialog.SetDefaultResponse("accept")

	dialog.ConnectResponse(func(response string) {
		res(response == "accept")
	})

	dialog.Present(gutil.PointerToWidgetter(a.window()))
}

type Info struct {
	Heading string
	Body    string
	Extra   func() gtk.Widgetter
}

func (d Info) Show(a *App, closed func()) {
	dialog := adw.NewAlertDialog(d.Heading, d.Body)
	dialog.SetBodyUseMarkup(true)
	dialog.AddResponse("close", "_Close")
	dialog.SetDefaultResponse("close")
	if d.Extra != nil {
		dialog.SetExtraChild(d.Extra())
	}

	if closed != nil {
		dialog.ConnectResponse(func(string) {
			closed()
		})
	}

	dialog.Present(gutil.PointerToWidgetter(a.window()))
}

func codeLabel(text string) gtk.Widgetter {
	w := gtk.NewLabel(text)
	w.AddCSSClass("monospace")
	w.AddCSSClass("frame")
	w.SetSelectable(true)
	w.SetWrap(true)
	return w
}
