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
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/lucasssvaz/traygolin/internal/olm"
	"github.com/lucasssvaz/traygolin/internal/pangolin"
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
	return hint + "\n<tt>sudo chown root:root " + html.EscapeString(shellQuote(cliPath)) + "</tt>\n\n" +
		"The second command is needed because the updater leaves the new CLI owned by you, " +
		"and Traygolin only connects with a root-owned CLI."
}

// shellQuote makes s safe to paste into a shell command.
func shellQuote(s string) string {
	safe := s != ""
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_@%+=:,./-", r)) {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// folderURI is the file URI of the folder holding path. A relative path is
// taken from the working directory, which is where TailLog reads it from.
func folderURI(path string) string {
	dir := filepath.Dir(path)
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(dir)}).String()
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
// official clients show. Servers past the second, which the CLI accepts
// but the fields have no room for, stay in the secondary field so that
// saving Preferences does not drop them.
func SplitUpstream(vals []string) (primary, secondary string) {
	if len(vals) > 0 {
		primary = vals[0]
	}
	if len(vals) > 1 {
		secondary = strings.Join(vals[1:], ", ")
	}
	return primary, secondary
}

// JoinUpstream is the inverse of SplitUpstream for `pangolin config set`.
// Either field may hold a comma-separated list.
func JoinUpstream(primary, secondary string) string {
	return strings.Join(append(pangolin.SplitCSV(primary), pangolin.SplitCSV(secondary)...), ",")
}
