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
	"strconv"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/lucasssvaz/traygolin/internal/gutil"
	"github.com/lucasssvaz/traygolin/internal/olm"
)

//go:embed sitepage.ui
var sitePageXML string

// SitePage shows one Pangolin site the tunnel reaches.
type SitePage struct {
	app    *App
	siteID int

	Page           *adw.StatusPage
	Connected      *gtk.Image
	ModeRow        *adw.ActionRow
	LatencyRow     *adw.ActionRow
	LastSeenRow    *adw.ActionRow
	EndpointRow    *adw.ActionRow
	PeerAddressRow *adw.ActionRow
	ExitNode       *gtk.Image

	actions      *gio.SimpleActionGroup
	copyEndpoint *gio.SimpleAction
	copyAddress  *gio.SimpleAction
	stackPage    *adw.ViewStackPage
	peer         olm.Peer
}

func sitePageName(id int) string { return "site:" + strconv.Itoa(id) }

// findPeer looks a site up by the ID its page was made with. That is the
// ID SortedPeers reports, which is not always the key of the peers map.
func findPeer(st *State, siteID int) (olm.Peer, bool) {
	for _, p := range st.Tunnel.Peers() {
		if p.SiteID == siteID {
			return p, true
		}
	}
	return olm.Peer{}, false
}

func NewSitePage(app *App, peer olm.Peer) *SitePage {
	page := SitePage{app: app, siteID: peer.SiteID, peer: peer}
	gutil.FillFromUI(&page, sitePageXML)

	page.actions = gio.NewSimpleActionGroup()
	page.copyEndpoint = gio.NewSimpleAction("copy-endpoint", nil)
	page.copyEndpoint.ConnectActivate(func(*glib.Variant) { app.copyText(page.peer.Endpoint, "Copied endpoint") })
	page.actions.AddAction(page.copyEndpoint)
	page.copyAddress = gio.NewSimpleAction("copy-address", nil)
	page.copyAddress.ConnectActivate(func(*glib.Variant) { app.copyText(page.peer.PeerIP, "Copied peer address") })
	page.actions.AddAction(page.copyAddress)

	return &page
}

func (page *SitePage) Widget() gtk.Widgetter {
	return page.Page
}

func (page *SitePage) Actions() gio.ActionGrouper {
	return page.actions
}

func (page *SitePage) Bind(stackPage *adw.ViewStackPage) {
	page.stackPage = stackPage
	page.bindTitle()
}

func (page *SitePage) bindTitle() {
	if page.stackPage == nil {
		return
	}
	page.stackPage.SetTitle(page.Page.Title())
	icon := "network-server-symbolic"
	if !page.peer.Connected {
		icon = "network-offline-symbolic"
	}
	page.stackPage.SetIconName(icon)
}

func (page *SitePage) Update(st *State) bool {
	if !st.Tunnel.Online() {
		return false
	}
	peer, ok := findPeer(st, page.siteID)
	if !ok {
		return false
	}
	page.peer = peer

	name := peer.Name
	if name == "" {
		name = "Site " + strconv.Itoa(peer.SiteID)
	}
	page.Page.SetTitle(name)
	page.Page.SetDescription(ModeText(peer))
	page.bindTitle()

	page.Connected.SetFromGIcon(boolIcon(peer.Connected))
	page.ModeRow.SetSubtitle(peer.Mode())
	rowText(page.LatencyRow, LatencyText(peer.RTT))
	rowText(page.LastSeenRow, AgoText(peer.LastSeen, time.Now()))
	rowText(page.EndpointRow, peer.Endpoint)
	rowText(page.PeerAddressRow, peer.PeerIP)
	page.ExitNode.SetFromGIcon(boolIcon(st.Tunnel.ExitNodeActive() && st.Tunnel.Status.IsGateway(peer.SiteID)))

	page.copyEndpoint.SetEnabled(peer.Endpoint != "")
	page.copyAddress.SetEnabled(peer.PeerIP != "")
	return true
}
