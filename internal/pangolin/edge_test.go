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
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestExtractDeviceCode(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Enter the code: ABCD-EFGH", "ABCD-EFGH"},
		{"enter the code ABCD-EFGH", "ABCD-EFGH"},
		{"Your device code is WXYZ-1234", "WXYZ-1234"},
		{"verification code: 123456", "123456"},
		{"One-time code: A1B2C3", "A1B2C3"},
		{"user code:   QRST-UVWX  ", "QRST-UVWX"},
		{"\x1b[1mEnter the code: ABCD-EFGH\x1b[0m", "ABCD-EFGH"},
		// Prose after a trigger phrase is not a device code.
		{"The code is valid for 10 minutes", ""},
		{"Your code is ready", ""},
		{"device code expires soon", ""},
		{"Enter the code on the page", ""},
		{"code: abc", ""},
		{"", ""},
		{"no trigger here ABCD-EFGH", ""},
	} {
		if got := ExtractDeviceCode(tc.in); got != tc.want {
			t.Errorf("ExtractDeviceCode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestExtractURLs(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"Visit https://app.example/device?code=ABCD", []string{"https://app.example/device?code=ABCD"}},
		{"open http://localhost:3000/x and https://b.example", []string{"http://localhost:3000/x", "https://b.example"}},
		// Punctuation that ends a sentence is not part of the link.
		{"Open https://app.example/device.", []string{"https://app.example/device"}},
		{"(see https://app.example/device)", []string{"https://app.example/device"}},
		{"Go to https://app.example/a, then continue", []string{"https://app.example/a"}},
		{"Go to <https://app.example/a>", []string{"https://app.example/a"}},
		{`"https://app.example/a"`, []string{"https://app.example/a"}},
		{"\x1b[4mhttps://app.example/a\x1b[0m", []string{"https://app.example/a"}},
		{"ftp://nope javascript:alert(1)", nil},
		{"", nil},
	} {
		got := ExtractURLs(tc.in)
		if !slices.Equal(got, tc.want) {
			t.Errorf("ExtractURLs(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRedactForms(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want []string
	}{
		{[]string{"up", "--secret", "hunter2"}, []string{"up", "--secret", "<redacted>"}},
		{[]string{"up", "--secret=hunter2"}, []string{"up", "--secret=<redacted>"}},
		{[]string{"--secret"}, []string{"--secret"}},
		{[]string{"--secret", "a", "--secret=b"}, []string{"--secret", "<redacted>", "--secret=<redacted>"}},
		{nil, []string{}},
	} {
		in := slices.Clone(tc.args)
		got := redact(tc.args)
		if len(got) != len(tc.want) || (len(got) > 0 && !slices.Equal(got, tc.want)) {
			t.Errorf("redact(%q) = %q, want %q", tc.args, got, tc.want)
		}
		if !slices.Equal(in, tc.args) {
			t.Errorf("redact modified its input: %q -> %q", in, tc.args)
		}
	}
	err := wrapRunError("pangolin", []string{"up", "--secret=hunter2"}, "", "boom", errors.New("exit 1"))
	if strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("secret leaked: %v", err)
	}
}

func TestRunErrorMessages(t *testing.T) {
	long := "l1\nl2\nl3\nl4\nl5"
	err := wrapRunError("pangolin", []string{"up"}, "", long, errors.New("exit 1"))
	if got := err.Error(); got != "pangolin up: l3 l4 l5" {
		t.Errorf("last three lines: %q", got)
	}
	err = wrapRunError("pangolin", []string{"up"}, "stdout only", "", errors.New("exit 1"))
	if got := err.Error(); got != "pangolin up: stdout only" {
		t.Errorf("stdout fallback: %q", got)
	}
	exit := errors.New("exit status 1")
	err = wrapRunError("pangolin", []string{"up"}, "", "", exit)
	if got := err.Error(); got != "pangolin up: exit status 1" || !errors.Is(err, exit) {
		t.Errorf("fallback to the process error: %q", got)
	}
	if (&RunError{Args: []string{"a"}}).Error() == "" {
		t.Error("empty RunError should still say something")
	}
}

func TestParseAccountsOrderIsStable(t *testing.T) {
	// The same person on two servers, as with Pangolin Cloud plus a
	// self-hosted instance. Map iteration order is random, so ties must be
	// broken by something stable or the tray menu reshuffles on every poll.
	const doc = `{"activeuserid":"b","accounts":{
	  "a": {"userId":"a","host":"https://cloud.example","email":"me@example.com"},
	  "b": {"userId":"b","host":"https://self.example","email":"me@example.com"},
	  "c": {"userId":"c","host":"https://self.example","email":"ME@example.com"}
	}}`
	first := parseAccounts([]byte(doc))
	if len(first.Accounts) != 3 {
		t.Fatalf("%+v", first)
	}
	for i := range 300 {
		got := parseAccounts([]byte(doc))
		for j := range first.Accounts {
			if got.Accounts[j].UserID != first.Accounts[j].UserID {
				t.Fatalf("run %d: order changed: %q then %q", i, userIDs(first.Accounts), userIDs(got.Accounts))
			}
		}
	}
}

func userIDs(accs []Account) []string {
	out := make([]string, len(accs))
	for i, a := range accs {
		out[i] = a.UserID
	}
	return out
}

func TestParseAccountsEdgeCases(t *testing.T) {
	for name, tc := range map[string]struct {
		doc      string
		loggedIn bool
		accounts int
		active   string
	}{
		"empty":               {"", false, 0, ""},
		"null":                {"null", false, 0, ""},
		"array":               {"[]", false, 0, ""},
		"truncated":           {`{"accounts":{"u":{"userId":"u"`, false, 0, ""},
		"no accounts":         {`{"activeuserid":"u"}`, false, 0, ""},
		"active is missing":   {`{"activeuserid":"ghost","accounts":{"u":{"userId":"u"}}}`, false, 1, ""},
		"active by key":       {`{"activeuserid":"k","accounts":{"k":{"userId":"u"}}}`, true, 1, "u"},
		"active by user id":   {`{"activeuserid":"u","accounts":{"k":{"userId":"u"}}}`, true, 1, "u"},
		"user id from key":    {`{"activeuserid":"k","accounts":{"k":{}}}`, true, 1, "k"},
		"empty active":        {`{"activeuserid":"","accounts":{"k":{}}}`, false, 1, ""},
		"unknown fields":      {`{"x":1,"accounts":{"k":{"userId":"k","new":{"a":[1,2]}}},"activeuserid":"k"}`, true, 1, "k"},
		"wrong account type":  {`{"accounts":{"k":"nope"}}`, false, 0, ""},
		"accounts is a list":  {`{"accounts":[{"userId":"k"}]}`, false, 0, ""},
		"utf-8 names":         {`{"activeuserid":"k","accounts":{"k":{"userId":"k","email":"jörg@exämple.com"}}}`, true, 1, "k"},
		"token is not copied": {`{"activeuserid":"k","accounts":{"k":{"userId":"k","sessionToken":"SECRET"}}}`, true, 1, "k"},
	} {
		t.Run(name, func(t *testing.T) {
			auth := parseAccounts([]byte(tc.doc))
			if auth.LoggedIn != tc.loggedIn || len(auth.Accounts) != tc.accounts || auth.Active.UserID != tc.active {
				t.Fatalf("%+v", auth)
			}
			if strings.Contains(fmt.Sprintf("%+v", auth), "SECRET") {
				t.Fatal("session token copied into Auth")
			}
		})
	}
}

func TestAccountLabelPriority(t *testing.T) {
	if got := (Account{}).Label(); got != "Account" {
		t.Errorf("empty label %q", got)
	}
	for _, tc := range []struct {
		acc  Account
		want string
	}{
		{Account{Email: "e", Username: "u", Name: "n", UserID: "i"}, "e"},
		{Account{Username: "u", Name: "n", UserID: "i"}, "u"},
		{Account{Name: "n", UserID: "i"}, "n"},
		{Account{UserID: "i"}, "i"},
	} {
		if got := tc.acc.Label(); got != tc.want {
			t.Errorf("%+v: %q, want %q", tc.acc, got, tc.want)
		}
	}
}

func TestParseCLIConfigEdgeCases(t *testing.T) {
	def := parseCLIConfig(nil)
	for name, doc := range map[string]string{
		"invalid":    "{",
		"null":       "null",
		"array":      "[1,2]",
		"string":     `"x"`,
		"up is text": `{"up":"nope"}`,
		"up is list": `{"up":[1]}`,
		"empty":      `{}`,
	} {
		if got := parseCLIConfig([]byte(doc)); !got.Equal(def) {
			t.Errorf("%s: %+v, want defaults %+v", name, got, def)
		}
	}

	cfg := parseCLIConfig([]byte(`{
	  "log_file": "",
	  "up.tunnel_dns": true,
	  "up": {"tunnel_dns": false, "override_dns": "false", "upstream_dns": "1.1.1.1, 8.8.8.8,,",
	         "match_domains_dns": ["", "  ", "*.a", 5, null], "prefer_local_routes": "yes", "exit_node_takes_precedence": 1}
	}`))
	if cfg.LogFile != DefaultLogPath() {
		t.Errorf("empty log_file should keep the default, got %q", cfg.LogFile)
	}
	if !cfg.TunnelDNS {
		t.Error("the flattened up.* key takes precedence over the nested one")
	}
	if cfg.OverrideDNS {
		t.Error(`"false" as a string is false`)
	}
	if !slices.Equal(cfg.UpstreamDNS, []string{"1.1.1.1", "8.8.8.8"}) {
		t.Errorf("csv upstreams %q", cfg.UpstreamDNS)
	}
	if !slices.Equal(cfg.MatchDomains, []string{"*.a"}) {
		t.Errorf("non-string and blank entries dropped: %q", cfg.MatchDomains)
	}
	if cfg.PreferLocalRoutes || cfg.ExitNodeTakesPrecedence {
		t.Errorf("unparsable booleans are false: %+v", cfg)
	}
}

func TestConfigEqual(t *testing.T) {
	base := CLIConfig{LogFile: "a", UpstreamDNS: []string{"x"}, MatchDomains: []string{"y"}}
	if !base.Equal(base) {
		t.Fatal("equal to itself")
	}
	for name, mutate := range map[string]func(*CLIConfig){
		"log":      func(c *CLIConfig) { c.LogFile = "b" },
		"tunnel":   func(c *CLIConfig) { c.TunnelDNS = true },
		"override": func(c *CLIConfig) { c.OverrideDNS = true },
		"up":       func(c *CLIConfig) { c.UpstreamDNS = []string{"x", "z"} },
		"match":    func(c *CLIConfig) { c.MatchDomains = nil },
		"local":    func(c *CLIConfig) { c.PreferLocalRoutes = true },
		"exit":     func(c *CLIConfig) { c.ExitNodeTakesPrecedence = true },
	} {
		other := base
		other.UpstreamDNS = slices.Clone(base.UpstreamDNS)
		other.MatchDomains = slices.Clone(base.MatchDomains)
		mutate(&other)
		if base.Equal(other) || other.Equal(base) {
			t.Errorf("%s change not noticed", name)
		}
	}
	// nil and empty slices are the same list.
	if !(CLIConfig{UpstreamDNS: []string{}}).Equal(CLIConfig{}) {
		t.Error("nil and empty differ")
	}
}

func TestSplitCSV(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"", nil},
		{",", nil},
		{" , ,", nil},
		{"a", []string{"a"}},
		{" a , b,,c ,", []string{"a", "b", "c"}},
		{"a b,c", []string{"a b", "c"}},
		{"*.proxy.internal,corp.example", []string{"*.proxy.internal", "corp.example"}},
	} {
		if got := SplitCSV(tc.in); !slices.Equal(got, tc.want) {
			t.Errorf("SplitCSV(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestUpArgsEdgeCases(t *testing.T) {
	for _, tc := range []struct {
		opts UpOptions
		want []string
	}{
		{UpOptions{Holepunch: true}, []string{"up", "--silent"}},
		{UpOptions{Holepunch: true, MTU: -5}, []string{"up", "--silent"}},
		{UpOptions{Holepunch: true, MTU: 0}, []string{"up", "--silent"}},
		{UpOptions{Holepunch: true, MTU: 1281}, []string{"up", "--silent", "--mtu", "1281"}},
		{UpOptions{Holepunch: true, MTU: 9000, DisableRelay: true}, []string{"up", "--silent", "--mtu", "9000", "--disable-relay"}},
		{UpOptions{}, []string{"up", "--silent", "--holepunch=false"}},
	} {
		if got := UpArgs(tc.opts); !slices.Equal(got, tc.want) {
			t.Errorf("%+v: %q, want %q", tc.opts, got, tc.want)
		}
	}
}

func TestClassifyHelperErrorEdgeCases(t *testing.T) {
	for _, tc := range []struct {
		msg  string
		want error
	}{
		{"Privileged Helper Is Not Installed", ErrHelperMissing},
		{"Authorization Was Denied", ErrNotAuthorized},
		{"pkexec: Not authorized", ErrNotAuthorized},
		{"RERUN THIS COMMAND AS SUDO", ErrDeviceSetup},
		// A message that mentions several cases resolves in a fixed order.
		{"the privileged helper is not installed; authorization was denied", ErrHelperMissing},
	} {
		if got := classifyHelperError(errors.New(tc.msg)); !errors.Is(got, tc.want) {
			t.Errorf("%q: %v, want %v", tc.msg, got, tc.want)
		}
	}
	wrapped := fmt.Errorf("outer: %w", &RunError{Args: []string{"up"}, Output: "authorization was denied"})
	if !errors.Is(classifyHelperError(wrapped), ErrNotAuthorized) {
		t.Error("wrapped RunError")
	}
}

func TestLatestVersionVariants(t *testing.T) {
	for _, tc := range []struct{ name, script, want string }{
		{"stderr notice", `echo 0.18.1; echo "A new version is available: 0.19.0 (current: 0.18.1)" >&2`, "0.19.0"},
		{"stdout notice", `echo "A new version is available: 0.20.1"`, "0.20.1"},
		{"v prefix", `echo "new version is available: v1.2.3"`, "1.2.3"},
		{"colored", `printf '\033[33mA new version is available: v2.0.0\033[0m\n'`, "2.0.0"},
		{"none", `echo 0.18.1`, ""},
		{"silent", `true`, ""},
	} {
		c := &Client{Binary: writeFakeCLI(t, tc.script)}
		got, err := c.LatestVersion(t.Context())
		if err != nil || got != tc.want {
			t.Errorf("%s: %q %v, want %q", tc.name, got, err, tc.want)
		}
	}
}

func TestVersionEdgeCases(t *testing.T) {
	for _, tc := range []struct{ name, script, want string }{
		{"first line only", `echo 0.18.1; echo "update available"`, "0.18.1"},
		{"leading blank", `echo; echo "0.18.1"`, "0.18.1"},
		{"stderr only", `echo 0.18.1 >&2`, "0.18.1"},
		{"padded", `echo "   0.18.1   "`, "0.18.1"},
		{"empty", `true`, ""},
	} {
		c := &Client{Binary: writeFakeCLI(t, tc.script)}
		got, err := c.Version(t.Context())
		if err != nil || got != tc.want {
			t.Errorf("%s: %q %v, want %q", tc.name, got, err, tc.want)
		}
	}
	if _, err := (&Client{Binary: writeFakeCLI(t, `echo boom >&2; exit 4`)}).Version(t.Context()); err == nil {
		t.Error("exit status 4 should fail")
	} else {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 4 {
			t.Errorf("exit error not reachable: %v", err)
		}
	}
}

func TestBinaryResolution(t *testing.T) {
	bin := writeFakeCLI(t, `echo from-env`)
	t.Setenv("PANGOLIN_BINARY", bin)
	var nilClient *Client
	if got := nilClient.bin(); got != bin {
		t.Errorf("nil client: %q", got)
	}
	if got := (&Client{}).bin(); got != bin {
		t.Errorf("empty client: %q", got)
	}
	if got := (&Client{Binary: "/x/y"}).bin(); got != "/x/y" {
		t.Errorf("explicit binary wins: %q", got)
	}
	t.Setenv("PANGOLIN_BINARY", "")
	if got := (&Client{}).bin(); got != "pangolin" {
		t.Errorf("default: %q", got)
	}
}

func TestEnvironmentReachesTheCLI(t *testing.T) {
	out := filepath.Join(t.TempDir(), "env")
	bin := writeFakeCLI(t, `echo "$TG_A:$TG_B" > `+out+`; echo ok`)
	t.Setenv("TG_A", "from-process")
	t.Setenv("TG_B", "from-process")
	c := &Client{Binary: bin, Env: []string{"TG_B=from-client"}}
	if _, err := c.output(t.Context(), 0, []string{"TG_B=from-call"}, "version"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(out)
	// Later entries win, so call-specific values override the client's.
	if got := strings.TrimSpace(string(data)); got != "from-process:from-call" {
		t.Errorf("env %q", got)
	}
}

func TestMissingAndBrokenBinaries(t *testing.T) {
	ctx := t.Context()
	c := &Client{Binary: filepath.Join(t.TempDir(), "nope")}
	if _, err := c.Version(ctx); err == nil {
		t.Error("missing binary")
	}
	if _, err := c.Login(ctx, "", nil); err == nil {
		t.Error("missing binary for stream")
	}
	notExec := filepath.Join(t.TempDir(), "pangolin")
	if err := os.WriteFile(notExec, []byte("#!/bin/sh\necho hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (&Client{Binary: notExec}).Version(ctx); err == nil {
		t.Error("non-executable binary")
	}
	badShebang := filepath.Join(t.TempDir(), "pangolin")
	if err := os.WriteFile(badShebang, []byte("#!/nonexistent/interp\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := (&Client{Binary: badShebang}).Version(ctx); err == nil {
		t.Error("bad shebang")
	}
	if err := (&Client{Binary: filepath.Join(t.TempDir(), "nope")}).Logout(ctx); err == nil {
		t.Error("logout with a missing binary")
	}
}

func TestTimeoutKillsTheCLIAndItsChildren(t *testing.T) {
	// The shell forks sleep, which keeps the output pipe open after the
	// shell is killed. WaitDelay has to cut that off.
	bin := writeFakeCLI(t, `sleep 8; echo late`)
	c := &Client{Binary: bin}
	start := time.Now()
	_, err := c.output(t.Context(), 200*time.Millisecond, nil, "version")
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("took %v to give up on a hung CLI", elapsed)
	}
}

func TestCanceledContextStopsStream(t *testing.T) {
	bin := writeFakeCLI(t, `echo "waiting"; sleep 8; echo done`)
	c := &Client{Binary: bin}
	ctx, cancel := context.WithCancel(t.Context())
	lines := make(chan string, 4)
	done := make(chan error, 1)
	go func() {
		_, err := c.Login(ctx, "", func(ev LoginEvent) { lines <- ev.Line })
		done <- err
	}()
	select {
	case <-lines:
	case <-time.After(5 * time.Second):
		t.Fatal("no output")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled login should report an error")
		}
	case <-time.After(4 * time.Second):
		t.Fatal("canceled login did not return promptly")
	}
}

func TestLoginDoesNotHangOnHugeOutput(t *testing.T) {
	// A line longer than the scanner's buffer stops the scan. The CLI must
	// still be drained, or it blocks on a full pipe until the timeout.
	bin := writeFakeCLI(t, `head -c 3000000 /dev/zero | tr '\0' 'a'; echo; echo "Enter the code: AFTER-BIGLINE"`)
	c := &Client{Binary: bin}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	start := time.Now()
	_, err := c.Login(ctx, "", nil)
	if elapsed := time.Since(start); elapsed > 8*time.Second {
		t.Fatalf("Login hung for %v on a very long line (err %v)", elapsed, err)
	}
}

func TestLoginEventDedupAndANSI(t *testing.T) {
	bin := writeFakeCLI(t, `printf '\033[1mOpen https://app.example/device\033[0m\n'
echo "Again: https://app.example/device and https://other.example/x"
echo "Enter the code: ABCD-EFGH"
echo "Enter the code: ABCD-EFGH"`)
	var urls, codes, plain []string
	_, err := (&Client{Binary: bin}).Login(t.Context(), "  ", func(ev LoginEvent) {
		if ev.URL != "" {
			urls = append(urls, ev.URL)
		}
		if ev.Code != "" {
			codes = append(codes, ev.Code)
		}
		plain = append(plain, ev.Line)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(urls, []string{"https://app.example/device", "https://other.example/x"}) {
		t.Errorf("each URL is reported once: %q", urls)
	}
	if len(codes) != 2 {
		t.Errorf("codes %q", codes)
	}
	for _, l := range plain {
		if strings.Contains(l, "\x1b") {
			t.Errorf("escape sequence left in %q", l)
		}
	}
}

func TestLoginArguments(t *testing.T) {
	log := filepath.Join(t.TempDir(), "args")
	bin := writeFakeCLI(t, `echo "$#:$@" > `+log)
	c := &Client{Binary: bin}
	for host, want := range map[string]string{
		"":                    "1:login",
		"   ":                 "1:login",
		" https://h.example ": "2:login https://h.example",
	} {
		if _, err := c.Login(t.Context(), host, nil); err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(log)
		if got := strings.TrimSpace(string(data)); got != want {
			t.Errorf("host %q: %q, want %q", host, got, want)
		}
	}
}

func TestSelectAccountFallbacks(t *testing.T) {
	log := filepath.Join(t.TempDir(), "args")
	c := &Client{Binary: writeFakeCLI(t, `echo "$@" > `+log)}
	for acc, want := range map[Account]string{
		{UserID: "u1"}:                          "select account --account u1",
		{UserID: "u1", Username: "bob"}:         "select account --account bob",
		{UserID: "u1", Email: "e@x", Host: "h"}: "select account --account e@x --host h",
	} {
		if err := c.SelectAccount(t.Context(), acc); err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(log)
		if got := strings.TrimSpace(string(data)); got != want {
			t.Errorf("%+v: %q, want %q", acc, got, want)
		}
	}
}

func TestArgumentsAreNotInterpretedByAShell(t *testing.T) {
	log := filepath.Join(t.TempDir(), "args")
	c := &Client{Binary: writeFakeCLI(t, `printf '%s|' "$@" > `+log)}
	evil := `org; touch ` + filepath.Join(filepath.Dir(log), "pwned") + ` $(id) "q" 'x'`
	if err := c.SelectOrg(t.Context(), evil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(log), "pwned")); err == nil {
		t.Fatal("argument reached a shell")
	}
	data, _ := os.ReadFile(log)
	if want := "select|org|--org|" + evil + "|"; string(data) != want {
		t.Errorf("args %q, want %q", data, want)
	}
}

func TestConcurrentCLICalls(t *testing.T) {
	c := &Client{Binary: writeFakeCLI(t, `echo 0.18.1`)}
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for range 48 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if v, err := c.Version(t.Context()); err != nil || v != "0.18.1" {
				errs <- fmt.Errorf("%q %v", v, err)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestConfigDirFallbacks(t *testing.T) {
	// The CLI keeps its files in ~/.config/pangolin whatever XDG_CONFIG_HOME
	// says. Following XDG here would read files the CLI never writes.
	t.Setenv("XDG_CONFIG_HOME", "/x/config")
	t.Setenv("HOME", "/home/someone")
	if got := ConfigDir(); got != "/home/someone/.config/pangolin" {
		t.Errorf("with XDG_CONFIG_HOME set: %q", got)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	if got := ConfigDir(); got != "/home/someone/.config/pangolin" {
		t.Errorf("home: %q", got)
	}
	if got := DefaultLogPath(); got != "/home/someone/.config/pangolin/logs/client.log" {
		t.Errorf("log path: %q", got)
	}
	// Without $HOME (as under some service managers) the CLI asks the user
	// database, and so does this.
	t.Setenv("HOME", "")
	if got := ConfigDir(); !filepath.IsAbs(got) || !strings.HasSuffix(got, filepath.Join(".config", "pangolin")) {
		t.Errorf("no HOME: %q", got)
	}
}

func TestUpDoesNotInheritOlmVariables(t *testing.T) {
	// Generic names from the desktop session must not turn into tunnel
	// flags. Other commands and Traygolin's own variables are unaffected.
	log := filepath.Join(t.TempDir(), "env")
	bin := writeFakeCLI(t, `env > `+log)
	for _, kv := range []string{"LOG_LEVEL=warn", "INTERFACE=wlan0", "MTU=1500", "HTTP_ADDR=:8080", "DNS=9.9.9.9", "SUBNET_ROUTER=true", "DISABLE_HOLEPUNCH=true", "UPSTREAM_DNS=1.1.1.1"} {
		name, value, _ := strings.Cut(kv, "=")
		t.Setenv(name, value)
	}
	t.Setenv("TRAYGOLIN_TEST_KEEP", "kept")
	t.Setenv("LOG_LEVELS", "not-an-olm-name")
	read := func() string {
		t.Helper()
		data, err := os.ReadFile(log)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	c := &Client{Binary: bin, Env: []string{"MTU=1400"}}
	if _, err := c.output(t.Context(), 5*time.Second, []string{"PATH=" + os.Getenv("PATH")}, UpArgs(UpOptions{})...); err != nil {
		t.Fatal(err)
	}
	env := "\n" + read()
	for _, name := range olmEnv {
		if strings.Contains(env, "\n"+name+"=") && !(name == "MTU" && strings.Contains(env, "\nMTU=1400\n")) {
			t.Errorf("up inherited %s", name)
		}
	}
	for _, want := range []string{"TRAYGOLIN_TEST_KEEP=kept", "LOG_LEVELS=not-an-olm-name", "MTU=1400"} {
		if !strings.Contains(env, want) {
			t.Errorf("up lost %s", want)
		}
	}
	if _, err := c.output(t.Context(), 5*time.Second, nil, "version"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(), "LOG_LEVEL=warn") {
		t.Error("only up is cleaned; other commands keep the environment")
	}
}

func TestLoadAuthAndConfigWithoutFiles(t *testing.T) {
	withConfigHome(t)
	if a := LoadAuth(); a.LoggedIn || len(a.Accounts) != 0 {
		t.Errorf("%+v", a)
	}
	if c := LoadConfig(); !c.OverrideDNS || c.LogFile == "" {
		t.Errorf("%+v", c)
	}
	if got := sessionCookieName(); got != "" {
		t.Errorf("cookie name %q", got)
	}
	if _, err := ServerClient("u1"); err == nil {
		t.Error("no accounts file")
	}
}

func TestServerClientEdgeCases(t *testing.T) {
	dir := withConfigHome(t)
	write := func(doc string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "accounts.json"), []byte(doc), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"accounts":{
	  "k1":{"userId":"u1","host":"h1","sessionToken":"t1"},
	  "k2":{"userId":"u2","host":"","sessionToken":"t2"},
	  "k3":{"userId":"u3","host":"h3","sessionToken":""}}}`)
	if c, err := ServerClient("k1"); err != nil || c.Token != "t1" {
		t.Errorf("lookup by map key: %v %v", c, err)
	}
	if c, err := ServerClient("u1"); err != nil || c.Token != "t1" {
		t.Errorf("lookup by user id: %v %v", c, err)
	}
	for _, id := range []string{"u2", "u3"} {
		if _, err := ServerClient(id); err == nil || err.Error() != "the Pangolin session has expired; log in again" {
			t.Errorf("%s without host or token: %v", id, err)
		}
	}
	if _, err := ServerClient("ghost"); err == nil {
		t.Error("unknown account")
	}
	if _, err := ServerClient(""); err == nil {
		t.Error("empty id must not match anything")
	}
	write(`{`)
	if _, err := ServerClient("u1"); err == nil {
		t.Error("corrupt file")
	}
}

func TestTailLogEdgeCases(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if out, err := TailLog(write("empty", ""), 100); err != nil || out != "" {
		t.Errorf("empty: %q %v", out, err)
	}
	if _, err := TailLog(filepath.Join(dir, "missing"), 100); err == nil {
		t.Error("missing file")
	}
	if _, err := TailLog(dir, 100); err == nil {
		t.Error("a directory is not a log")
	}
	if out, _ := TailLog(write("nonl", "a\nb"), 100); out != "a\nb\n" {
		t.Errorf("no trailing newline: %q", out)
	}
	if out, _ := TailLog(write("crlf", "a\r\nb\r\n"), 100); out != "a\nb\n" {
		t.Errorf("crlf: %q", out)
	}
	if out, _ := TailLog(write("blank", "a\n\n\nb\n"), 100); out != "a\n\n\nb\n" {
		t.Errorf("blank lines: %q", out)
	}
	exact := "aa\nbb\ncc\n"
	if out, _ := TailLog(write("exact", exact), len(exact)); out != exact {
		t.Errorf("window equal to the file: %q", out)
	}
	if out, _ := TailLog(write("one-over", exact), len(exact)-1); out != "bb\ncc\n" {
		t.Errorf("window one byte short drops the first line: %q", out)
	}
	if out, _ := TailLog(write("tiny", exact), 1); out != "" {
		t.Errorf("a window inside the last line has no complete line: %q", out)
	}
	if out, _ := TailLog(write("multibyte", "héllo wörld\nsecond\n"), 12); out != "second\n" {
		t.Errorf("window cutting a multi-byte rune: %q", out)
	}
	if out, _ := TailLog(write("default", "x\n"), 0); out != "x\n" {
		t.Errorf("default window: %q", out)
	}
	if out, _ := TailLog(write("negative", "x\n"), -5); out != "x\n" {
		t.Errorf("negative window: %q", out)
	}
	big := strings.Repeat("l\n", 200000)
	out, err := TailLog(write("big", big), 1000)
	if err != nil || len(out) > 1000 || !strings.HasSuffix(out, "l\n") || strings.Contains(out, "ll") {
		t.Errorf("big file tail: %d bytes, %v", len(out), err)
	}
}

func TestTailLogIsAlwaysWholeLinesOfTheEnd(t *testing.T) {
	// Property: the result is the longest run of complete lines at the end
	// of the file that fits in the window.
	path := filepath.Join(t.TempDir(), "log")
	rng := rand.New(rand.NewSource(7))
	for range 400 {
		var lines []string
		var content strings.Builder
		for range rng.Intn(12) {
			l := strings.Repeat(string(rune('a'+rng.Intn(26))), rng.Intn(9))
			lines = append(lines, l)
			content.WriteString(l + "\n")
		}
		if err := os.WriteFile(path, []byte(content.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		window := 1 + rng.Intn(content.Len()+3)
		got, err := TailLog(path, window)
		if err != nil {
			t.Fatal(err)
		}
		var suffix []string
		size := 0
		for i := len(lines) - 1; i >= 0 && size+len(lines[i])+1 <= window; i-- {
			suffix = append([]string{lines[i]}, suffix...)
			size += len(lines[i]) + 1
		}
		expect := ""
		for _, l := range suffix {
			expect += l + "\n"
		}
		if got != expect {
			t.Fatalf("window %d over %q\n got %q\nwant %q", window, content.String(), got, expect)
		}
	}
}

func TestLoginReturnsWhenTheCLIExitsButAChildLivesOn(t *testing.T) {
	// The real CLI opens a browser that inherits its output pipes. Login
	// must not wait for the browser to quit.
	bin := writeFakeCLI(t, `echo "Enter the code: ZZZZ-1111"; sleep 8 &`)
	var code string
	start := time.Now()
	_, err := (&Client{Binary: bin}).Login(t.Context(), "", func(ev LoginEvent) {
		if ev.Code != "" {
			code = ev.Code
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if code != "ZZZZ-1111" {
		t.Errorf("code %q", code)
	}
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Fatalf("Login waited %v for a background child", elapsed)
	}
}
