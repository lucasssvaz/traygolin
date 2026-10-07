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
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lucasssvaz/traygolin/internal/server"
)

func writeFakeCLI(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pangolin")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func withConfigHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	p := filepath.Join(dir, "pangolin")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

const accountsJSON = `{
  "activeuserid": "u1",
  "accounts": {
    "u2": {"userId": "u2", "host": "https://b.example", "email": "zed@example.com"},
    "u1": {
      "userId": "u1", "host": "https://pangolin.example", "email": "user@example.com",
      "username": "user", "name": "User", "orgId": "org",
      "sessionToken": "SECRET-TOKEN",
      "olmCredentials": {"id": "olm", "secret": "nope"},
      "exitNodeResourceId": 9
    }
  }
}`

func TestLoadAuth(t *testing.T) {
	dir := withConfigHome(t)
	if err := os.WriteFile(filepath.Join(dir, "accounts.json"), []byte(accountsJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	auth := LoadAuth()
	if !auth.LoggedIn || auth.Active.Email != "user@example.com" || auth.Active.OrgID != "org" {
		t.Fatalf("%+v", auth)
	}
	if len(auth.Accounts) != 2 || auth.Accounts[0].Label() != "user@example.com" || auth.Accounts[1].Label() != "zed@example.com" {
		t.Fatalf("%+v", auth.Accounts)
	}
	if auth.Active.ExitNodeResourceID != 9 || auth.Accounts[1].ExitNodeResourceID != 0 {
		t.Fatalf("saved exit node %+v", auth.Accounts)
	}
}

func TestServerClient(t *testing.T) {
	dir := withConfigHome(t)
	if err := os.WriteFile(filepath.Join(dir, "accounts.json"), []byte(accountsJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := ServerClient("u1")
	if err != nil {
		t.Fatal(err)
	}
	if c.Host != "https://pangolin.example" || c.Token != "SECRET-TOKEN" || c.CookieName != "" {
		t.Fatalf("%+v", c)
	}
	if _, err := ServerClient("u2"); !errors.Is(err, server.ErrUnauthorized) {
		t.Fatalf("account without a session: %v", err)
	}
	if _, err := ServerClient("nobody"); err == nil {
		t.Fatal("unknown account")
	}

	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"session_cookie_name":"custom"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if c, _ := ServerClient("u1"); c.CookieName != "custom" {
		t.Fatalf("cookie name %q", c.CookieName)
	}
}

func TestLoadAuthLoggedOut(t *testing.T) {
	dir := withConfigHome(t)
	if LoadAuth().LoggedIn {
		t.Fatal("no file means logged out")
	}
	os.WriteFile(filepath.Join(dir, "accounts.json"), []byte(`{"activeuserid":"","accounts":{"u":{"userId":"u"}}}`), 0o600)
	if LoadAuth().LoggedIn {
		t.Fatal("no active user means logged out")
	}
}

func TestClearSavedExitNode(t *testing.T) {
	dir := withConfigHome(t)
	path := filepath.Join(dir, "accounts.json")
	if err := os.WriteFile(path, []byte(accountsJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ClearSavedExitNode(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	var doc struct {
		Active   string                                `json:"activeuserid"`
		Accounts map[string]map[string]json.RawMessage `json:"accounts"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc.Accounts["u1"]["exitNodeResourceId"]; ok {
		t.Fatal("exit node not cleared")
	}
	if string(doc.Accounts["u1"]["sessionToken"]) != `"SECRET-TOKEN"` || doc.Active != "u1" {
		t.Fatal("other fields lost")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode())
	}
}

func TestParseCLIConfig(t *testing.T) {
	cfg := parseCLIConfig([]byte(`{
  "log_file": "/tmp/client.log",
  "up": {
    "tunnel_dns": true,
    "override_dns": false,
    "upstream_dns": ["10.0.0.53", "10.0.0.54"],
    "match_domains_dns": ["*.internal"],
    "prefer_local_routes": true,
    "exit_node_takes_precedence": true
  }
}`))
	if cfg.LogFile != "/tmp/client.log" || !cfg.TunnelDNS || cfg.OverrideDNS || len(cfg.UpstreamDNS) != 2 || !cfg.PreferLocalRoutes || !cfg.ExitNodeTakesPrecedence {
		t.Fatalf("%+v", cfg)
	}
	def := parseCLIConfig(nil)
	if !def.OverrideDNS || def.TunnelDNS || def.LogFile == "" {
		t.Fatalf("defaults %+v", def)
	}
	if !cfg.Equal(cfg) || cfg.Equal(def) {
		t.Fatal("Equal")
	}
}

func TestUpArgs(t *testing.T) {
	got := UpArgs(UpOptions{MTU: 1280, Holepunch: true})
	if !slices.Equal(got, []string{"up", "--silent"}) {
		t.Fatalf("%q", got)
	}
	got = UpArgs(UpOptions{MTU: 1400, Holepunch: false, DisableRelay: true})
	if !slices.Equal(got, []string{"up", "--silent", "--mtu", "1400", "--holepunch=false", "--disable-relay"}) {
		t.Fatalf("%q", got)
	}
}

func TestClassifyUpError(t *testing.T) {
	err := &RunError{Args: []string{"up"}, Output: "traygolin: the privileged helper is not installed. Open Traygolin"}
	if !errors.Is(classifyUpError(err), ErrHelperMissing) {
		t.Fatal("helper missing")
	}
	err = &RunError{Args: []string{"up"}, Output: "traygolin: authorization was denied or dismissed"}
	if !errors.Is(classifyUpError(err), ErrNotAuthorized) {
		t.Fatal("denied")
	}
}

func TestRunErrorRedactsSecret(t *testing.T) {
	err := wrapRunError("pangolin", []string{"up", "--secret", "hunter2"}, "", "boom", errors.New("exit 1"))
	if strings.Contains(err.Error(), "hunter2") {
		t.Fatal(err)
	}
}

func TestFakeCLICommands(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls")
	bin := writeFakeCLI(t, `echo "$@" >> `+log+`
case "$1" in
  version) echo "0.99.0" ;;
  login) echo "Visit https://app.example/device"; echo "Enter the code: ABCD-EFGH" ;;
esac
exit 0`)
	c := &Client{Binary: bin}
	ctx := t.Context()
	if v, err := c.Version(ctx); err != nil || v != "0.99.0" {
		t.Fatalf("%q %v", v, err)
	}
	var urls, codes []string
	if _, err := c.Login(ctx, "https://app.example", func(ev LoginEvent) {
		if ev.URL != "" {
			urls = append(urls, ev.URL)
		}
		if ev.Code != "" {
			codes = append(codes, ev.Code)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(urls, []string{"https://app.example/device"}) || !slices.Equal(codes, []string{"ABCD-EFGH"}) {
		t.Fatalf("%q %q", urls, codes)
	}
	if err := c.SelectAccount(ctx, Account{UserID: "u1", Email: "a@b", Host: "https://h"}); err != nil {
		t.Fatal(err)
	}
	if err := c.SelectOrg(ctx, "org2"); err != nil {
		t.Fatal(err)
	}
	if err := c.ConfigSet(ctx, "up.tunnel_dns", "true"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(log)
	for _, want := range []string{"select account --account a@b --host https://h", "select org --org org2", "config set up.tunnel_dns true"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("missing %q in\n%s", want, data)
		}
	}
}

func TestTailLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.log")
	os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o644)
	if out, err := TailLog(path, 1024); err != nil || out != "one\ntwo\nthree\n" {
		t.Fatalf("%q %v", out, err)
	}
	if out, _ := TailLog(path, 6); out != "three\n" {
		t.Fatalf("aligned cut: %q", out)
	}
	if out, _ := TailLog(path, 8); out != "three\n" {
		t.Fatalf("partial first line should be dropped: %q", out)
	}
}

func TestLookPathMissing(t *testing.T) {
	if _, err := (&Client{Binary: "/this/pangolin/does/not/exist"}).LookPath(); err == nil {
		t.Fatal("expected missing")
	}
}
