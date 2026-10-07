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
	"slices"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/lucasssvaz/traygolin/internal/tray"
)

// Choice is one entry of the organization or exit node dropdowns. An
// empty ID on an exit node choice means None.
type Choice struct {
	ID    string
	Label string
}

// OrgChoices lists the organizations to offer and the index of the one in
// use. Without a server list, only the current organization is offered.
func OrgChoices(st *State) ([]Choice, int) {
	current := tray.CurrentOrg(*st)
	var choices []Choice
	if st.Server != nil {
		for _, o := range st.Server.Orgs {
			choices = append(choices, Choice{ID: o.ID, Label: o.Label()})
		}
	}
	i := slices.IndexFunc(choices, func(c Choice) bool { return c.ID == current })
	if i < 0 && current != "" {
		choices = append(choices, Choice{ID: current, Label: tray.OrgLabel(*st)})
		i = len(choices) - 1
	}
	return choices, i
}

// ExitChoices lists None and the exit nodes to offer, and the index of
// the one in use. A saved exit node the server no longer lists is kept so
// the selection stays truthful.
func ExitChoices(st *State) ([]Choice, int) {
	current := tray.CurrentExitNode(*st)
	choices := []Choice{{Label: "None"}}
	selected := 0
	if st.Server != nil {
		for _, e := range st.Server.ExitNodes {
			choices = append(choices, Choice{ID: e.NiceID, Label: tray.ExitNodeOptionLabel(e)})
			if e.ResourceID == current {
				selected = len(choices) - 1
			}
		}
	}
	if current != 0 && selected == 0 {
		choices = append(choices, Choice{Label: tray.ExitNodeLabel(*st)})
		selected = len(choices) - 1
	}
	return choices, selected
}

// CurrentExitNiceID is the nice ID of the exit node in use, or "".
func CurrentExitNiceID(st *State) string {
	if e, ok := st.Server.ExitNode(tray.CurrentExitNode(*st)); ok {
		return e.NiceID
	}
	return ""
}

// choiceRow is an AdwComboRow bound to a list of Choices.
type choiceRow struct {
	Row      *adw.ComboRow
	model    *gtk.StringList
	choices  []Choice
	updating bool
}

func newChoiceRow(title string, onSelect func(Choice)) *choiceRow {
	r := &choiceRow{
		Row:   adw.NewComboRow(),
		model: gtk.NewStringList(nil),
	}
	r.Row.SetTitle(title)
	r.Row.SetUseSubtitle(true)
	r.Row.AddCSSClass("property")
	r.Row.SetModel(r.model)
	r.Row.NotifyProperty("selected", func() {
		if r.updating {
			return
		}
		if i := int(r.Row.Selected()); i >= 0 && i < len(r.choices) {
			onSelect(r.choices[i])
		}
	})
	return r
}

func (r *choiceRow) set(choices []Choice, selected int, sensitive bool) {
	r.updating = true
	defer func() { r.updating = false }()

	if !slices.Equal(r.choices, choices) {
		labels := make([]string, len(choices))
		for i, c := range choices {
			labels[i] = c.Label
		}
		r.choices = slices.Clone(choices)
		r.model.Splice(0, r.model.NItems(), labels)
	}
	pos := uint(gtk.InvalidListPosition)
	if selected >= 0 {
		pos = uint(selected)
	}
	if r.Row.Selected() != pos {
		r.Row.SetSelected(pos)
	}
	r.Row.SetSensitive(sensitive && len(choices) > 1)
}

// ChoiceRows are the organization and exit node dropdowns. The device and
// offline pages each own one.
type ChoiceRows struct {
	Org  *choiceRow
	Exit *choiceRow
}

func NewChoiceRows(app *App) *ChoiceRows {
	return &ChoiceRows{
		Org: newChoiceRow("Organization", func(c Choice) {
			app.selectOrg(c.ID)
		}),
		Exit: newChoiceRow("Exit Node", func(c Choice) {
			if c.ID != "" || c.Label == "None" {
				app.selectExitNode(c.ID)
			}
		}),
	}
}

const exitNodeHint = "Route all internet traffic through a site in your organization."

// Update refreshes both rows. The exit node row stays visible but
// insensitive when the server has no exit nodes. It returns the
// description for the exit node row's group: a hint, or why exit nodes
// are unavailable.
func (c *ChoiceRows) Update(st *State) string {
	loggedIn := st.Accounts != nil && st.Accounts.Auth.LoggedIn
	reason := tray.ExitNodesUnavailable(*st)
	desc := exitNodeHint
	if reason != "" {
		desc = reason + "."
	}
	if st.Busy != "" {
		// Keep showing what the user just picked until the change lands.
		c.Org.Row.SetSensitive(false)
		c.Exit.Row.SetSensitive(false)
		return desc
	}

	orgs, org := OrgChoices(st)
	c.Org.set(orgs, org, loggedIn)

	exits, exit := ExitChoices(st)
	c.Exit.set(exits, exit, loggedIn && reason == "")
	return desc
}
