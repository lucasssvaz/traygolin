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
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/lucasssvaz/traygolin/internal/tray"
)

// State is the latest of each poller status kind.
type State = tray.State

var (
	boolTrueIcon  = gio.NewThemedIconWithDefaultFallbacks("emblem-ok-symbolic")
	boolFalseIcon = gio.NewThemedIconWithDefaultFallbacks("window-close-symbolic")
)

func boolIcon(v bool) gio.Iconner {
	if v {
		return boolTrueIcon
	}
	return boolFalseIcon
}

// Page is the UI for a single sidebar entry.
type Page interface {
	Widget() gtk.Widgetter
	Actions() gio.ActionGrouper

	// Bind attaches the page to a ViewStackPage. It is called each time
	// the page is inserted into the stack and must be safe to call more
	// than once.
	Bind(*adw.ViewStackPage)

	// Update refreshes the page. It returns false if the page should be
	// removed.
	Update(*State) bool
}

// rowText sets a property row's subtitle, hiding the row when empty.
func rowText(row *adw.ActionRow, text string) {
	row.SetVisible(text != "")
	if row.Subtitle() != text {
		row.SetSubtitle(text)
	}
}
