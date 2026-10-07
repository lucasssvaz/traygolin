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

// Copy matches official Pangolin client wording where the docs publish it.
var Copy = struct {
	StartAtLogin, StartAtLoginDesc     string
	ConnectAtStart, ConnectAtStartDesc string
	TrayIcon, TrayIconDesc             string
	PollTitle, PollDesc                string
	Helper, HelperDesc                 string

	EnableAliases, EnableAliasesDesc string
	TunnelDNS, TunnelDNSDesc         string
	PrimaryDNS, PrimaryDNSDesc       string
	SecondaryDNS, SecondaryDNSDesc   string
	MatchDomains, MatchDomainsDesc   string
	ResetDNS, ResetDNSDesc           string

	PreferLocal, PreferLocalDesc   string
	ExitPrecede, ExitPrecedeDesc   string
	Holepunch, HolepunchDesc       string
	DisableRelay, DisableRelayDesc string
	MTU, MTUDesc                   string

	Disclaimer, LogoWhy string
}{
	StartAtLogin:       "Start at Login",
	StartAtLoginDesc:   "Open Traygolin in the tray when you sign in to this desktop.",
	ConnectAtStart:     "Connect at Start",
	ConnectAtStartDesc: "Connect automatically whenever Traygolin starts, if you are logged in.",
	TrayIcon:           "Show Tray Icon",
	TrayIconDesc:       "Show Traygolin in the system tray. Closing the window keeps it running either way.",
	PollTitle:          "Status Refresh Interval",
	PollDesc:           "Seconds between status checks. Traygolin checks every second while connecting.",
	Helper:             "Passwordless Connect",
	HelperDesc:         "A small system helper and polkit rule let you connect without an administrator password, like the official clients.",

	EnableAliases:     "Enable Aliases (Override DNS)",
	EnableAliasesDesc: "Required to use aliases on Pangolin resources. Queries that are not Pangolin resources go to your Upstream DNS Server.",
	TunnelDNS:         "DNS Over Tunnel",
	TunnelDNSDesc:     "Send DNS queries through the tunnel to a private DNS server published as a Pangolin resource. Set its IP as Upstream DNS. Requires Enable Aliases.",
	PrimaryDNS:        "Primary Upstream DNS",
	PrimaryDNSDesc:    "Leave blank to use System DNS.",
	SecondaryDNS:      "Secondary Upstream DNS",
	SecondaryDNSDesc:  "Fallback resolver. Leave blank to use System DNS.",
	MatchDomains:      "Match Domains",
	MatchDomainsDesc:  "Comma-separated. When set, only matching names (such as *.proxy.internal) use Upstream DNS; everything else uses system DNS.",
	ResetDNS:          "Reset DNS",
	ResetDNSDesc:      "Clear DNS overrides left behind if the client stopped unexpectedly.",

	PreferLocal:      "Prefer Local Routes",
	PreferLocalDesc:  "Keep LAN addresses (printers, NAS) local when a Pangolin route overlaps your network.",
	ExitPrecede:      "Exit Nodes Take Precedence Over Resources",
	ExitPrecedeDesc:  "When on, all traffic and DNS go through the exit node and other resources become unreachable.",
	Holepunch:        "Holepunch",
	HolepunchDesc:    "Try a direct peer-to-peer path through NAT.",
	DisableRelay:     "Disable Relay",
	DisableRelayDesc: "Never fall back to relaying through Pangolin. Sites that cannot holepunch stay unreachable.",
	MTU:              "MTU",
	MTUDesc:          "Default 1280. Every site must use the same value. Applies on the next connection.",

	Disclaimer: "Traygolin is an unofficial app and is not affiliated with Fossorial or Pangolin. It requires the official Pangolin CLI.",
	LogoWhy:    "Pangolin and Fossorial names and logos are trademarks of their owners and are not used in Traygolin's icon.",
}
