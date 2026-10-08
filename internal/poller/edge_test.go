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

package poller

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lucasssvaz/traygolin/internal/olm"
	"github.com/lucasssvaz/traygolin/internal/pangolin"
	"github.com/lucasssvaz/traygolin/internal/server"
)

// countingOlm serves h on a socket and counts the requests it gets.
func countingOlm(t *testing.T, h http.HandlerFunc) (string, *atomic.Int64) {
	t.Helper()
	dir, err := os.MkdirTemp("", "tgp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "olm.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	var n atomic.Int64
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		h(w, r)
	})}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return sock, &n
}

func okStatus(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(`{"connected":true,"registered":true}`))
}

// collect gathers everything the poller publishes.
type collect struct {
	mu  sync.Mutex
	all []Status
}

func (c *collect) add(s Status) {
	c.mu.Lock()
	c.all = append(c.all, s)
	c.mu.Unlock()
}

func (c *collect) cli() (out []*CLIStatus) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, s := range c.all {
		if v, ok := s.(*CLIStatus); ok {
			out = append(out, v)
		}
	}
	return out
}

func (c *collect) accounts() (out []*AccountStatus) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, s := range c.all {
		if v, ok := s.(*AccountStatus); ok {
			out = append(out, v)
		}
	}
	return out
}

func eventually(t *testing.T, within time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func start(t *testing.T, p *Poller) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()
	return func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("Run did not return after the context ended")
		}
	}
}

func TestNonPositiveIntervalsDoNotSpin(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for name, set := range map[string]time.Duration{
		"zero":     0,
		"negative": -time.Second,
		"tiny":     time.Nanosecond,
	} {
		t.Run(name, func(t *testing.T) {
			sock, hits := countingOlm(t, okStatus)
			p := &Poller{Interval: time.Hour, Olm: olm.New(sock), CLI: &pangolin.Client{Binary: "/does/not/exist"}}
			stop := start(t, p)
			defer stop()
			select {
			case <-p.GetTunnel():
			case <-time.After(3 * time.Second):
				t.Fatal("no first status")
			}
			p.SetInterval() <- set
			base := hits.Load()
			time.Sleep(700 * time.Millisecond)
			// At the one-second minimum that is at most one or two refreshes.
			if n := hits.Load() - base; n > 10 {
				t.Fatalf("%d status requests in 700 ms: the poller is spinning", n)
			}
		})
	}
}

func TestStartingIntervalIsClamped(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for name, iv := range map[string]time.Duration{"zero": 0, "negative": -5, "tiny": time.Microsecond} {
		t.Run(name, func(t *testing.T) {
			sock, hits := countingOlm(t, okStatus)
			p := &Poller{Interval: iv, Olm: olm.New(sock), CLI: &pangolin.Client{Binary: "/does/not/exist"}}
			stop := start(t, p)
			defer stop()
			time.Sleep(700 * time.Millisecond)
			if n := hits.Load(); n > 10 {
				t.Fatalf("%d status requests in 700 ms", n)
			}
		})
	}
}

func TestFastIgnoresNonPositiveDurations(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sock, hits := countingOlm(t, okStatus)
	p := &Poller{Interval: time.Hour, Olm: olm.New(sock), CLI: &pangolin.Client{Binary: "/does/not/exist"}}
	stop := start(t, p)
	defer stop()
	<-p.GetTunnel()
	p.Fast() <- 0
	p.Fast() <- -time.Hour
	base := hits.Load()
	time.Sleep(1500 * time.Millisecond)
	if n := hits.Load() - base; n > 3 {
		t.Fatalf("%d refreshes with an hour interval and no fast mode", n)
	}
}

func TestRunEndsAndReleasesGoroutines(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sock, _ := countingOlm(t, okStatus)
	runtime.GC()
	before := runtime.NumGoroutine()
	for range 5 {
		p := &Poller{
			Interval: time.Hour,
			Olm:      olm.New(sock),
			CLI:      &pangolin.Client{Binary: "/does/not/exist"},
			New:      func(Status) {},
		}
		stop := start(t, p)
		select {
		case <-p.GetTunnel():
		case <-time.After(3 * time.Second):
			t.Fatal("no first status")
		}
		p.Fast() <- time.Second
		stop()
	}
	eventually(t, 5*time.Second, "goroutines to exit", func() bool {
		runtime.GC()
		return runtime.NumGoroutine() <= before+2
	})
}

func TestRunWithAlreadyCanceledContext(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	p := &Poller{Interval: time.Hour, Olm: olm.New(filepath.Join(t.TempDir(), "none")), CLI: &pangolin.Client{Binary: "/does/not/exist"}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run ignored a canceled context")
	}
}

func TestCLIVersionIsRefreshedAfterAnUpgradeInPlace(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	bin := filepath.Join(t.TempDir(), "pangolin")
	write := func(version string, mod time.Time) {
		t.Helper()
		if err := os.WriteFile(bin, []byte("#!/bin/sh\necho "+version+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		os.Chtimes(bin, mod, mod)
	}
	write("0.18.1", time.Now().Add(-time.Hour))
	var c collect
	p := &Poller{Interval: time.Hour, Olm: olm.New(filepath.Join(t.TempDir(), "none")), CLI: &pangolin.Client{Binary: bin}, New: c.add}
	stop := start(t, p)
	defer stop()
	eventually(t, 5*time.Second, "the first CLI status", func() bool {
		s := c.cli()
		return len(s) > 0 && s[len(s)-1].Version == "0.18.1"
	})
	// "pangolin update" replaces the binary where it is.
	write("0.19.0", time.Now())
	<-p.Poll()
	eventually(t, 5*time.Second, "the new version to show", func() bool {
		s := c.cli()
		return s[len(s)-1].Version == "0.19.0"
	})
}

func TestCLIVersionIsRetriedAfterAFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	marker := filepath.Join(t.TempDir(), "failed-once")
	bin := filepath.Join(t.TempDir(), "pangolin")
	script := fmt.Sprintf("#!/bin/sh\nif [ ! -f %q ]; then touch %q; exit 1; fi\necho 0.18.1\n", marker, marker)
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	var c collect
	p := &Poller{Interval: time.Hour, Olm: olm.New(filepath.Join(t.TempDir(), "none")), CLI: &pangolin.Client{Binary: bin}, New: c.add}
	stop := start(t, p)
	defer stop()
	eventually(t, 5*time.Second, "the first CLI status", func() bool { return len(c.cli()) > 0 })
	if v := c.cli()[0].Version; v != "" {
		t.Fatalf("first call was meant to fail, got %q", v)
	}
	<-p.Poll()
	eventually(t, 5*time.Second, "the version to be fetched on the next refresh", func() bool {
		s := c.cli()
		return s[len(s)-1].Version == "0.18.1"
	})
}

func TestUnchangedStateIsPublishedOnce(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var c collect
	p := &Poller{
		Interval: time.Hour,
		Olm:      olm.New(filepath.Join(t.TempDir(), "none")),
		CLI:      fakeCLI(t),
		New:      c.add,
	}
	stop := start(t, p)
	defer stop()
	eventually(t, 5*time.Second, "first statuses", func() bool { return len(c.cli()) > 0 && len(c.accounts()) > 0 })
	for range 25 {
		<-p.Poll()
	}
	time.Sleep(200 * time.Millisecond)
	if n := len(c.cli()); n != 1 {
		t.Errorf("CLI status published %d times for no change", n)
	}
	if n := len(c.accounts()); n != 1 {
		t.Errorf("account status published %d times for no change", n)
	}
}

func TestAccountChangesAreSeenOnTheNextPoll(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var c collect
	p := &Poller{Interval: time.Hour, Olm: olm.New(filepath.Join(t.TempDir(), "none")), CLI: fakeCLI(t), New: c.add,
		Server: func(string) (ServerAPI, error) { return nil, errors.New("offline") }}
	stop := start(t, p)
	defer stop()
	eventually(t, 5*time.Second, "first account status", func() bool { return len(c.accounts()) > 0 })
	if c.accounts()[0].Auth.LoggedIn {
		t.Fatal("nothing is logged in yet")
	}
	writeAccounts(t, "home")
	<-p.Poll()
	eventually(t, 5*time.Second, "login to show up", func() bool {
		a := c.accounts()
		return a[len(a)-1].Auth.LoggedIn
	})
	if err := os.Remove(filepath.Join(pangolin.ConfigDir(), "accounts.json")); err != nil {
		t.Fatal(err)
	}
	<-p.Poll()
	eventually(t, 5*time.Second, "logout to show up", func() bool {
		a := c.accounts()
		return !a[len(a)-1].Auth.LoggedIn
	})
}

func TestLoggedOutMakesNoServerCalls(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var calls atomic.Int64
	var c collect
	p := &Poller{
		Interval: time.Hour,
		Olm:      olm.New(filepath.Join(t.TempDir(), "none")),
		CLI:      &pangolin.Client{Binary: "/does/not/exist"},
		Server:   func(string) (ServerAPI, error) { calls.Add(1); return nil, errors.New("must not be called") },
		New:      c.add,
	}
	stop := start(t, p)
	defer stop()
	for range 5 {
		<-p.Poll()
	}
	time.Sleep(100 * time.Millisecond)
	if calls.Load() != 0 {
		t.Fatalf("%d server clients were created without an account", calls.Load())
	}
}

func TestServerSetupFailureIsReportedOnce(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeAccounts(t, "home")
	var mu sync.Mutex
	var servers []*ServerStatus
	p := &Poller{
		Interval: time.Hour,
		Olm:      olm.New(filepath.Join(t.TempDir(), "none")),
		CLI:      &pangolin.Client{Binary: "/does/not/exist"},
		Server:   func(string) (ServerAPI, error) { return nil, server.ErrUnauthorized },
		New: func(s Status) {
			if v, ok := s.(*ServerStatus); ok {
				mu.Lock()
				servers = append(servers, v)
				mu.Unlock()
			}
		},
	}
	stop := start(t, p)
	defer stop()
	for range 5 {
		<-p.Poll()
	}
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(servers) != 1 || !errors.Is(servers[0].Err, server.ErrUnauthorized) {
		t.Fatalf("%d statuses, first %+v", len(servers), servers)
	}
	if servers[0].Info.Version != "1.21.1" {
		t.Errorf("the version saved at login should still be shown: %+v", servers[0].Info)
	}
}

func TestSwitchingAccountNeverShowsTheOldAccountsData(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeAccounts(t, "home")
	api := &fakeServerAPI{exitOrgs: make(chan string, 64)}
	var mu sync.Mutex
	var servers []*ServerStatus
	p := &Poller{
		Interval: time.Hour,
		Olm:      olm.New(filepath.Join(t.TempDir(), "none")),
		CLI:      &pangolin.Client{Binary: "/does/not/exist"},
		Server:   func(string) (ServerAPI, error) { return api, nil },
		New: func(s Status) {
			if v, ok := s.(*ServerStatus); ok {
				mu.Lock()
				servers = append(servers, v)
				mu.Unlock()
			}
		},
	}
	stop := start(t, p)
	defer stop()
	last := func() *ServerStatus {
		mu.Lock()
		defer mu.Unlock()
		if len(servers) == 0 {
			return nil
		}
		return servers[len(servers)-1]
	}
	eventually(t, 5*time.Second, "first server status", func() bool { s := last(); return s != nil && len(s.ExitNodes) == 1 })

	// A different user on the same host, whose requests fail.
	api.fail.Store(true)
	dir := pangolin.ConfigDir()
	other := `{"activeuserid":"u2","accounts":{"u2":{"userId":"u2","host":"https://p.example","email":"b@example.com","sessionToken":"t","orgId":"home"}}}`
	if err := os.WriteFile(filepath.Join(dir, "accounts.json"), []byte(other), 0o600); err != nil {
		t.Fatal(err)
	}
	<-p.Poll()
	eventually(t, 5*time.Second, "status for the new user", func() bool { s := last(); return s != nil && s.UserID == "u2" })
	s := last()
	if len(s.Orgs) != 0 || len(s.ExitNodes) != 0 || s.Err == nil {
		t.Fatalf("user u2 is shown user u1's data: %+v", s)
	}
}

func TestFetchServerPartialFailure(t *testing.T) {
	api := &partialAPI{}
	p := &Poller{Server: func(string) (ServerAPI, error) { return api, nil }}
	s := p.fetchServer(t.Context(), pangolin.Account{UserID: "u", OrgID: "o", Server: server.Info{Version: "1.0.0"}})
	if s.Err == nil || len(s.Orgs) != 1 || s.Info.Version != "1.30.0" {
		t.Fatalf("%+v", s)
	}
	if s.ExitNodes != nil {
		t.Errorf("failed exit node lookup should leave the list empty: %+v", s.ExitNodes)
	}
	// Without an organization there are no exit nodes to ask for.
	api.exitCalls = 0
	s = p.fetchServer(t.Context(), pangolin.Account{UserID: "u"})
	if api.exitCalls != 0 || s.Err != nil {
		t.Errorf("exit nodes requested without an org (%d calls): %+v", api.exitCalls, s)
	}
	// An account without a user ID is not asked about anything.
	if s := p.fetchServer(t.Context(), pangolin.Account{}); s.Err != nil || s.Orgs != nil {
		t.Errorf("%+v", s)
	}
}

type partialAPI struct{ exitCalls int }

func (a *partialAPI) Info(context.Context) (server.Info, error) {
	return server.Info{Version: "1.30.0"}, nil
}
func (a *partialAPI) Orgs(context.Context, string) ([]server.Org, error) {
	return []server.Org{{ID: "o"}}, nil
}
func (a *partialAPI) ExitNodes(context.Context, string) ([]server.ExitNode, error) {
	a.exitCalls++
	return nil, errors.New("exit nodes are not supported")
}

func TestFetchServerKeepsSavedVersionWhenInfoIsEmpty(t *testing.T) {
	p := &Poller{Server: func(string) (ServerAPI, error) { return emptyInfoAPI{}, nil }}
	s := p.fetchServer(t.Context(), pangolin.Account{UserID: "u", Server: server.Info{Version: "1.21.1", Build: "oss"}})
	if s.Info.Version != "1.21.1" {
		t.Fatalf("%+v", s.Info)
	}
}

type emptyInfoAPI struct{}

func (emptyInfoAPI) Info(context.Context) (server.Info, error) { return server.Info{}, nil }
func (emptyInfoAPI) Orgs(context.Context, string) ([]server.Org, error) {
	return nil, nil
}
func (emptyInfoAPI) ExitNodes(context.Context, string) ([]server.ExitNode, error) {
	return nil, nil
}

func TestFetchTunnelClassification(t *testing.T) {
	p := &Poller{}
	p.init()

	p.Olm = olm.New(filepath.Join(t.TempDir(), "none"))
	if s := p.fetchTunnel(t.Context()); s.Running || s.Err != nil || s.Status != nil {
		t.Errorf("no socket: %+v", s)
	}

	sock, _ := countingOlm(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	p.Olm = olm.New(sock)
	if s := p.fetchTunnel(t.Context()); s.Running || s.Err == nil {
		t.Errorf("broken olm: %+v", s)
	}

	sock, _ = countingOlm(t, okStatus)
	p.Olm = olm.New(sock)
	if s := p.fetchTunnel(t.Context()); !s.Running || !s.Online() || s.Err != nil {
		t.Errorf("healthy olm: %+v", s)
	}

	if os.Geteuid() != 0 {
		dir := t.TempDir()
		path := filepath.Join(dir, "olm.sock")
		ln, err := net.Listen("unix", path)
		if err != nil {
			t.Fatal(err)
		}
		defer ln.Close()
		os.Chmod(dir, 0)
		defer os.Chmod(dir, 0o700)
		p.Olm = olm.New(path)
		s := p.fetchTunnel(t.Context())
		if !s.Running || !s.Unreadable() || s.Online() {
			t.Errorf("unreadable socket should look like a tunnel we cannot inspect: %+v", s)
		}
	}
}

func TestTunnelStatusHelpersAreNilSafe(t *testing.T) {
	var s *TunnelStatus
	if s.Online() || s.Connecting() || s.Unreadable() || s.ExitNodeActive() || s.Peers() != nil {
		t.Fatal("nil status")
	}
	empty := &TunnelStatus{}
	if empty.Online() || empty.Connecting() || empty.Unreadable() || empty.ExitNodeActive() || empty.Peers() != nil {
		t.Fatal("empty status")
	}
	for name, tc := range map[string]struct {
		s                  TunnelStatus
		online, connecting bool
	}{
		"up":              {TunnelStatus{Running: true, Status: &olm.Status{Connected: true, Registered: true}}, true, false},
		"registering":     {TunnelStatus{Running: true, Status: &olm.Status{Connected: true}}, false, true},
		"terminated":      {TunnelStatus{Running: true, Status: &olm.Status{Terminated: true}}, false, false},
		"not running":     {TunnelStatus{Status: &olm.Status{}}, false, false},
		"running no data": {TunnelStatus{Running: true}, false, false},
	} {
		if tc.s.Online() != tc.online || tc.s.Connecting() != tc.connecting {
			t.Errorf("%s: online %v connecting %v", name, tc.s.Online(), tc.s.Connecting())
		}
	}
	up := &TunnelStatus{Running: true, Status: &olm.Status{Connected: true, Registered: true, GatewayActive: true}}
	if !up.ExitNodeActive() {
		t.Error("exit node active")
	}
	up.Status.GatewayActive = false
	if up.ExitNodeActive() {
		t.Error("exit node inactive")
	}
	if !(&TunnelStatus{Err: fmt.Errorf("x: %w", olm.ErrPermission)}).Unreadable() {
		t.Error("wrapped permission error")
	}
}

func TestStatusEquality(t *testing.T) {
	a := &ServerStatus{UserID: "u", OrgID: "o", Orgs: []server.Org{{ID: "a"}, {ID: "b"}}, ExitNodes: []server.ExitNode{{ResourceID: 1, SiteIDs: []int{1, 2}}}}
	b := &ServerStatus{UserID: "u", OrgID: "o", Orgs: []server.Org{{ID: "a"}, {ID: "b"}}, ExitNodes: []server.ExitNode{{ResourceID: 1, SiteIDs: []int{1, 2}}}}
	if !a.equal(b) || !b.equal(a) || !a.equal(a) {
		t.Error("equal copies")
	}
	var nilStatus *ServerStatus
	if a.equal(nil) || nilStatus.equal(a) || !nilStatus.equal(nil) {
		t.Error("nil handling")
	}
	for name, mutate := range map[string]func(*ServerStatus){
		"user":      func(s *ServerStatus) { s.UserID = "x" },
		"org":       func(s *ServerStatus) { s.OrgID = "x" },
		"info":      func(s *ServerStatus) { s.Info.Version = "9" },
		"org order": func(s *ServerStatus) { s.Orgs[0], s.Orgs[1] = s.Orgs[1], s.Orgs[0] },
		"orgs":      func(s *ServerStatus) { s.Orgs = s.Orgs[:1] },
		"site":      func(s *ServerStatus) { s.ExitNodes[0].SiteIDs[1] = 9 },
		"error":     func(s *ServerStatus) { s.Err = errors.New("x") },
	} {
		c := &ServerStatus{UserID: "u", OrgID: "o", Orgs: []server.Org{{ID: "a"}, {ID: "b"}}, ExitNodes: []server.ExitNode{{ResourceID: 1, SiteIDs: []int{1, 2}}}}
		mutate(c)
		if a.equal(c) {
			t.Errorf("%s change not noticed", name)
		}
	}
	// Two errors with the same text are the same state.
	x := &ServerStatus{Err: errors.New("offline")}
	y := &ServerStatus{Err: errors.New("offline")}
	if !x.equal(y) {
		t.Error("errors compared by identity")
	}

	c1 := &CLIStatus{Path: "/a", Version: "1"}
	c2 := &CLIStatus{Path: "/a", Version: "1"}
	if !c1.equal(c2) || c1.equal(&CLIStatus{Path: "/b", Version: "1"}) || c1.equal(&CLIStatus{Path: "/a", Version: "2"}) ||
		c1.equal(&CLIStatus{Path: "/a", Version: "1", HelperInstalled: true}) || c1.equal(&CLIStatus{Path: "/a", Version: "1", Err: errors.New("x")}) {
		t.Error("CLI status equality")
	}
	if (*CLIStatus)(nil).Found() || (&CLIStatus{Path: "/a", Err: errors.New("x")}).Found() || (&CLIStatus{}).Found() {
		t.Error("Found")
	}
}

func TestNotifierWakesEveryWaiter(t *testing.T) {
	root := newNotifier()
	const waiters = 50
	var woke atomic.Int64
	var wg sync.WaitGroup
	for range waiters {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n := root
			for range 3 {
				<-n.notify
				woke.Add(1)
				n = n.next
			}
		}()
	}
	n := root
	for range 3 {
		time.Sleep(5 * time.Millisecond)
		n = n.Notify()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("only %d of %d wake-ups", woke.Load(), waiters*3)
	}
}

func TestClampIntervalProperties(t *testing.T) {
	for _, d := range []time.Duration{-time.Hour, -1, 0, 1, time.Millisecond, MinInterval - 1, MinInterval, DefaultInterval, MaxInterval, MaxInterval + 1, 1<<63 - 1} {
		got := ClampInterval(d)
		if got < MinInterval || got > MaxInterval {
			t.Errorf("ClampInterval(%v) = %v", d, got)
		}
		if d >= MinInterval && d <= MaxInterval && got != d {
			t.Errorf("ClampInterval(%v) changed a valid value to %v", d, got)
		}
	}
}

func TestPollerStress(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeAccounts(t, "home")
	var online atomic.Bool
	online.Store(true)
	sock, hits := countingOlm(t, func(w http.ResponseWriter, r *http.Request) {
		if online.Load() {
			okStatus(w, r)
			return
		}
		w.Write([]byte(`{"connected":false}`))
	})
	api := &fakeServerAPI{exitOrgs: make(chan string, 4096)}
	var events atomic.Int64
	p := &Poller{
		Interval: time.Hour,
		Olm:      olm.New(sock),
		CLI:      fakeCLI(t),
		Server:   func(string) (ServerAPI, error) { return api, nil },
		New:      func(Status) { events.Add(1) },
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()

	var wg sync.WaitGroup
	deadline := time.Now().Add(1500 * time.Millisecond)
	var gets, nexts atomic.Int64
	for i := range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(deadline) {
				switch i % 6 {
				case 0:
					select {
					case <-p.Poll():
					case <-time.After(50 * time.Millisecond):
					}
				case 1:
					select {
					case p.Fast() <- time.Duration(i) * 100 * time.Millisecond:
					case <-time.After(50 * time.Millisecond):
					}
				case 2:
					select {
					case p.SetInterval() <- time.Duration(1+i%4) * time.Second:
					case <-time.After(50 * time.Millisecond):
					}
				case 3:
					select {
					case s := <-p.GetTunnel():
						if s == nil {
							t.Error("nil tunnel status")
						}
						gets.Add(1)
					case <-time.After(50 * time.Millisecond):
					}
				case 4:
					select {
					case s := <-p.NextTunnel():
						if s == nil {
							t.Error("nil tunnel status")
						}
						nexts.Add(1)
					case <-time.After(50 * time.Millisecond):
					}
				case 5:
					online.Store(!online.Load())
					time.Sleep(5 * time.Millisecond)
				}
			}
		}()
	}
	// The exit-node lookups must not back up and block the poller.
	go func() {
		for {
			select {
			case <-api.exitOrgs:
			case <-ctx.Done():
				return
			}
		}
	}()
	wg.Wait()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop")
	}
	if gets.Load() == 0 {
		t.Error("GetTunnel never answered")
	}
	if events.Load() == 0 || hits.Load() == 0 {
		t.Errorf("nothing was published (%d events, %d requests)", events.Load(), hits.Load())
	}
}

func TestSlowNewCallbackDoesNotStopPolling(t *testing.T) {
	// The UI callback may take a moment to hand work to the main loop.
	t.Setenv("HOME", t.TempDir())
	sock, hits := countingOlm(t, okStatus)
	var calls atomic.Int64
	p := &Poller{
		Interval: time.Hour,
		Olm:      olm.New(sock),
		CLI:      &pangolin.Client{Binary: "/does/not/exist"},
		New: func(s Status) {
			if _, ok := s.(*TunnelStatus); ok {
				calls.Add(1)
				time.Sleep(300 * time.Millisecond)
			}
		},
	}
	stop := start(t, p)
	defer stop()
	eventually(t, 3*time.Second, "first status", func() bool { return calls.Load() > 0 })
	// Other watchers keep working while the tunnel callback is slow.
	done := make(chan struct{})
	go func() { <-p.Poll(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Poll blocked behind a slow callback")
	}
	_ = hits
}

func TestCLIVersionIsNotAskedForOnEveryRefresh(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	calls := filepath.Join(t.TempDir(), "calls")
	bin := filepath.Join(t.TempDir(), "pangolin")
	script := fmt.Sprintf("#!/bin/sh\necho x >> %q\necho 0.18.1\n", calls)
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	var c collect
	p := &Poller{Interval: time.Hour, Olm: olm.New(filepath.Join(t.TempDir(), "none")), CLI: &pangolin.Client{Binary: bin}, New: c.add}
	stop := start(t, p)
	defer stop()
	eventually(t, 5*time.Second, "the CLI status", func() bool { return len(c.cli()) > 0 })
	for range 10 {
		<-p.Poll()
	}
	time.Sleep(200 * time.Millisecond)
	data, _ := os.ReadFile(calls)
	if n := len(data) / 2; n != 1 {
		t.Fatalf("the CLI was run %d times to read a version that did not change", n)
	}
}
