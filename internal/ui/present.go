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
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/lucasssvaz/traygolin/internal/olm"
)

// ModeText explains how this device reaches a site.
func ModeText(p olm.Peer) string {
	switch p.Mode() {
	case "Local":
		return "Local · same network"
	case "Relay":
		return "Relay · through Pangolin"
	default:
		return "Direct · peer-to-peer"
	}
}

// LatencyText formats a round-trip time.
func LatencyText(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	if d < time.Millisecond {
		return "< 1 ms"
	}
	return fmt.Sprintf("%d ms", d.Round(time.Millisecond).Milliseconds())
}

// AgoText formats t relative to now.
func AgoText(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d < 5*time.Second:
		return "Just now"
	case d < time.Minute:
		return fmt.Sprintf("%d seconds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	}
	return t.Local().Format("Jan 2, 15:04")
}

// SitesText summarises connected sites.
func SitesText(peers []olm.Peer) string {
	if len(peers) == 0 {
		return "No sites"
	}
	n := 0
	for _, p := range peers {
		if p.Connected {
			n++
		}
	}
	return fmt.Sprintf("%d of %d connected", n, len(peers))
}

// updateHint is Pango markup telling the user how to update the CLI at
// cliPath. Traygolin never runs `pangolin update` itself: it needs root
// and pipes a downloaded script into bash, and on a packaged /usr/bin
// install it would overwrite files the package manager owns.
func updateHint(cliPath string) string {
	if strings.HasPrefix(cliPath, "/usr/bin/") {
		return "Update it with your system package manager."
	}
	hint := "Updating needs administrator rights, so run this in a terminal:\n\n<tt>pangolin update</tt>"
	if cliPath == "" {
		return hint
	}
	return hint + "\n<tt>sudo chown root:root " + html.EscapeString(cliPath) + "</tt>\n\n" +
		"The second command is needed because the updater leaves the new CLI owned by you, " +
		"and Traygolin only connects with a root-owned CLI."
}

// ClientText names the CLI and tunnel versions.
func ClientText(cliVersion string, st *olm.Status) string {
	parts := []string{}
	if v := strings.TrimSpace(cliVersion); v != "" {
		parts = append(parts, "Pangolin CLI "+strings.TrimPrefix(v, "pangolin "))
	}
	if st != nil && st.Version != "" {
		parts = append(parts, "olm "+st.Version)
	}
	return strings.Join(parts, " · ")
}

// SplitUpstream splits the CLI's upstream list into the two fields the
// official clients show.
func SplitUpstream(vals []string) (primary, secondary string) {
	if len(vals) > 0 {
		primary = vals[0]
	}
	if len(vals) > 1 {
		secondary = vals[1]
	}
	return primary, secondary
}

// JoinUpstream is the inverse of SplitUpstream for `pangolin config set`.
func JoinUpstream(primary, secondary string) string {
	var parts []string
	if p := strings.TrimSpace(primary); p != "" {
		parts = append(parts, p)
	}
	if s := strings.TrimSpace(secondary); s != "" {
		parts = append(parts, s)
	}
	return strings.Join(parts, ",")
}
