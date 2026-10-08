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

package pangolin

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/lucasssvaz/traygolin/internal/server"
)

// Account is a logged-in Pangolin user stored by the CLI.
// Secrets from accounts.json are never copied into this struct.
type Account struct {
	UserID   string
	Host     string
	Email    string
	Username string
	Name     string
	OrgID    string
	Active   bool
	// ExitNodeResourceID is the exit node saved for the next connection,
	// or 0.
	ExitNodeResourceID int
	// Server is what the CLI recorded about the server at login.
	Server server.Info
}

// Label is how the official clients show an account.
func (a Account) Label() string {
	for _, s := range []string{a.Email, a.Username, a.Name, a.UserID} {
		if s != "" {
			return s
		}
	}
	return "Account"
}

// Auth is the login state derived from accounts.json.
type Auth struct {
	LoggedIn bool
	Active   Account
	Accounts []Account
}

type accountRecord struct {
	UserID   string `json:"userId"`
	Host     string `json:"host"`
	Email    string `json:"email"`
	Username string `json:"username"`
	Name     string `json:"name"`
	OrgID    string `json:"orgId"`

	ExitNodeResourceID int `json:"exitNodeResourceId"`
	ServerInfo         struct {
		Version string `json:"version"`
		Build   string `json:"build"`
	} `json:"serverInfo"`
}

type accountsFile struct {
	Accounts     map[string]accountRecord `json:"accounts"`
	ActiveUserID string                   `json:"activeuserid"`
}

// ServerClient returns an API client for the saved account userID. The
// session token is read from accounts.json for this call only.
func ServerClient(userID string) (*server.Client, error) {
	if userID == "" {
		// A record without a userId would otherwise match.
		return nil, errors.New("no account given")
	}
	data, err := os.ReadFile(accountsPath())
	if err != nil {
		return nil, err
	}
	var file struct {
		Accounts map[string]struct {
			UserID       string `json:"userId"`
			Host         string `json:"host"`
			SessionToken string `json:"sessionToken"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	// The map key wins over a userId field, and keys are tried in order, so
	// the same file always gives the same account.
	rec, ok := file.Accounts[userID]
	if !ok {
		ids := make([]string, 0, len(file.Accounts))
		for id := range file.Accounts {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			if file.Accounts[id].UserID == userID {
				rec, ok = file.Accounts[id], true
				break
			}
		}
	}
	if !ok {
		return nil, fmt.Errorf("account %s is not saved", userID)
	}
	if rec.Host == "" || rec.SessionToken == "" {
		return nil, server.ErrUnauthorized
	}
	return &server.Client{Host: rec.Host, Token: rec.SessionToken, CookieName: sessionCookieName()}, nil
}

// LoadAuth reads accounts.json without spawning the CLI.
func LoadAuth() Auth {
	data, err := os.ReadFile(accountsPath())
	if err != nil {
		return Auth{}
	}
	return parseAccounts(data)
}

func parseAccounts(data []byte) Auth {
	var file accountsFile
	if err := json.Unmarshal(data, &file); err != nil {
		return Auth{}
	}
	var auth Auth
	for id, rec := range file.Accounts {
		acc := Account{
			UserID:   rec.UserID,
			Host:     rec.Host,
			Email:    rec.Email,
			Username: rec.Username,
			Name:     rec.Name,
			OrgID:    rec.OrgID,

			ExitNodeResourceID: rec.ExitNodeResourceID,
			Server:             server.Info{Version: rec.ServerInfo.Version, Build: rec.ServerInfo.Build},
		}
		if acc.UserID == "" {
			acc.UserID = id
		}
		acc.Active = file.ActiveUserID != "" && (id == file.ActiveUserID || acc.UserID == file.ActiveUserID)
		if acc.Active {
			auth.Active = acc
			auth.LoggedIn = true
		}
		auth.Accounts = append(auth.Accounts, acc)
	}
	// Map iteration order is random, so ties (the same person on two
	// servers) are broken by host and ID to keep the menu from reshuffling.
	sort.Slice(auth.Accounts, func(i, j int) bool {
		x, y := auth.Accounts[i], auth.Accounts[j]
		if lx, ly := strings.ToLower(x.Label()), strings.ToLower(y.Label()); lx != ly {
			return lx < ly
		}
		if x.Host != y.Host {
			return x.Host < y.Host
		}
		return x.UserID < y.UserID
	})
	return auth
}

var (
	reURL = regexp.MustCompile(`https?://[^\s<>"'\x1b]+`)
	// The trigger phrase is matched without regard to case, the code itself is
	// not: device codes are upper case, and prose such as "the code is valid
	// for 10 minutes" must not be mistaken for one.
	reCode = regexp.MustCompile(`(?i:enter the code|user code|device code|one-time code|verification code|code is|code:)\s*([A-Z0-9][A-Z0-9-]{3,})`)
	reANSI = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)
)

// LoginEvent is a streamed line from `pangolin login`.
type LoginEvent struct {
	Line string
	// URL is the first URL in Line not seen on an earlier line, and URLs
	// lists every such URL in order.
	URL  string
	URLs []string
	Code string
}

// ExtractURLs returns every http(s) URL in text.
func ExtractURLs(text string) []string {
	urls := reURL.FindAllString(text, -1)
	for i, u := range urls {
		urls[i] = trimURL(u)
	}
	return urls
}

// trimURL drops punctuation that surrounds a link in prose, such as the full
// stop at the end of a sentence or the bracket that closes a parenthesis.
func trimURL(u string) string {
	for {
		trimmed := strings.TrimRight(u, ".,;:!?")
		if strings.HasSuffix(trimmed, ")") && strings.Count(trimmed, "(") < strings.Count(trimmed, ")") {
			trimmed = trimmed[:len(trimmed)-1]
		}
		if trimmed == u {
			return u
		}
		u = trimmed
	}
}

// ExtractDeviceCode returns a device login code in text, if any.
func ExtractDeviceCode(text string) string {
	if m := reCode.FindStringSubmatch(text); len(m) == 2 {
		return m[1]
	}
	return ""
}

// Login runs `pangolin login [host]` and streams URLs and device codes.
func (c *Client) Login(ctx context.Context, host string, onEvent func(LoginEvent)) (string, error) {
	args := []string{"login"}
	if host = strings.TrimSpace(host); host != "" {
		args = append(args, host)
	}
	seen := map[string]bool{}
	return c.stream(ctx, 5*time.Minute, func(line string) {
		if onEvent == nil {
			return
		}
		line = reANSI.ReplaceAllString(line, "")
		ev := LoginEvent{Line: line, Code: ExtractDeviceCode(line)}
		for _, u := range ExtractURLs(line) {
			if !seen[u] {
				seen[u] = true
				ev.URLs = append(ev.URLs, u)
			}
		}
		if len(ev.URLs) > 0 {
			ev.URL = ev.URLs[0]
		}
		onEvent(ev)
	}, args...)
}

func (c *Client) Logout(ctx context.Context) error {
	return c.run(ctx, 20*time.Second, "logout")
}

func (c *Client) SelectAccount(ctx context.Context, acc Account) error {
	name := acc.Email
	if name == "" {
		name = acc.Username
	}
	if name == "" {
		name = acc.UserID
	}
	args := []string{"select", "account", "--account", name}
	if acc.Host != "" {
		args = append(args, "--host", acc.Host)
	}
	return c.run(ctx, 20*time.Second, args...)
}

func (c *Client) SelectOrg(ctx context.Context, org string) error {
	return c.run(ctx, 20*time.Second, "select", "org", "--org", org)
}

// SelectExitNode saves an exit node by nice ID and applies it live if a
// tunnel is running.
func (c *Client) SelectExitNode(ctx context.Context, niceID string) error {
	return c.run(ctx, 20*time.Second, "select", "exit-node", "--exit-node", niceID)
}

// ClearSavedExitNode removes the saved exit node from the active account
// in accounts.json. The CLI only offers this through an interactive menu.
func ClearSavedExitNode() error {
	path := accountsPath()
	// The file may be a link into a dotfiles repository. Write through the
	// link rather than replacing it with a regular file.
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		return err
	}
	var active string
	_ = json.Unmarshal(doc["activeuserid"], &active)
	if len(doc["accounts"]) == 0 {
		return nil
	}
	var accounts map[string]map[string]json.RawMessage
	if err := json.Unmarshal(doc["accounts"], &accounts); err != nil {
		return err
	}
	if active == "" {
		return nil
	}
	// The active user is named by its map key or by its userId, as in
	// parseAccounts.
	acc, ok := accounts[active]
	if !ok {
		for _, candidate := range accounts {
			var id string
			if json.Unmarshal(candidate["userId"], &id) == nil && id == active {
				acc, ok = candidate, true
				break
			}
		}
	}
	if !ok {
		return nil
	}
	changed := false
	for k := range acc {
		if strings.EqualFold(k, "exitNodeResourceId") {
			delete(acc, k)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	raw, err := json.Marshal(accounts)
	if err != nil {
		return err
	}
	doc["accounts"] = raw
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".accounts-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// TailLog returns up to maxBytes from the end of the log at path.
func TailLog(path string, maxBytes int) (string, error) {
	if path == "" {
		path = DefaultLogPath()
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if maxBytes <= 0 {
		maxBytes = 64 * 1024
	}
	start := info.Size() - int64(maxBytes)
	if start < 0 {
		start = 0
	}
	first := false
	if start > 0 {
		prev := make([]byte, 1)
		if _, err := f.ReadAt(prev, start-1); err != nil {
			return "", err
		}
		first = prev[0] != '\n'
	}
	if _, err := f.Seek(start, 0); err != nil {
		return "", err
	}
	var b strings.Builder
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if first {
			first = false
			continue
		}
		b.WriteString(sc.Text())
		b.WriteByte('\n')
	}
	return b.String(), sc.Err()
}
