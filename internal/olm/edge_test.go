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

package olm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// serve starts an HTTP server on a fresh unix socket and returns a client.
func serve(t *testing.T, h http.Handler) *Client {
	t.Helper()
	dir, err := os.MkdirTemp("", "olm")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "olm.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: h}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return New(sock)
}

func reply(code int, body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(code)
		io.WriteString(w, body)
	})
}

func TestNewDefaultsToTheSystemSocket(t *testing.T) {
	if New("").Socket != DefaultSocket {
		t.Fatal("default socket")
	}
	if New("/x/y.sock").Socket != "/x/y.sock" {
		t.Fatal("explicit socket")
	}
}

func TestStatusDecodingEdgeCases(t *testing.T) {
	for name, tc := range map[string]struct {
		code    int
		body    string
		wantErr bool
	}{
		"empty object":      {200, `{}`, false},
		"null":              {200, `null`, false},
		"null peers":        {200, `{"peers":null}`, false},
		"unknown fields":    {200, `{"future":{"a":[1]},"connected":true}`, false},
		"empty body":        {200, ``, true},
		"truncated":         {200, `{"connected":tr`, true},
		"html":              {200, `<html>`, true},
		"array":             {200, `[]`, true},
		"wrong type":        {200, `{"connected":"yes"}`, true},
		"500":               {500, `{"error":"boom"}`, true},
		"404":               {404, `not found`, true},
		"403":               {403, ``, true},
		"extra after value": {200, `{"connected":true} trailing`, false},
		"zero time":         {200, `{"peers":{"1":{"lastSeen":"0001-01-01T00:00:00Z"}}}`, false},
		"gateway ids":       {200, `{"gatewaySiteIds":[1,2,3],"gatewayActive":true}`, false},
	} {
		t.Run(name, func(t *testing.T) {
			c := serve(t, reply(tc.code, tc.body))
			st, err := c.Status(t.Context())
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, want error %v (status %+v)", err, tc.wantErr, st)
			}
			if err == nil {
				// None of the accessors may panic on a sparse status.
				_ = st.Online()
				_ = st.SortedPeers()
				_ = st.TunnelIP()
				_ = st.IsGateway(1)
			}
		})
	}
}

func TestHTTPErrorMentionsTheServerMessage(t *testing.T) {
	c := serve(t, reply(500, "  something broke \n"))
	_, err := c.Status(t.Context())
	if err == nil || !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "something broke") {
		t.Fatalf("%v", err)
	}
	// A huge error page is cut, not copied into the message.
	c = serve(t, reply(500, strings.Repeat("x", 1<<20)))
	_, err = c.Status(t.Context())
	if err == nil || len(err.Error()) > 8192 {
		t.Fatalf("error message of %d bytes", len(fmt.Sprint(err)))
	}
}

func TestNilAndEmptyStatus(t *testing.T) {
	var st *Status
	if st.Online() || st.SortedPeers() != nil || st.TunnelIP() != "" || st.IsGateway(1) {
		t.Fatal("nil status")
	}
	empty := &Status{}
	if empty.Online() || len(empty.SortedPeers()) != 0 || empty.TunnelIP() != "" || empty.IsGateway(0) {
		t.Fatal("empty status")
	}
}

func TestOnlineNeedsEverything(t *testing.T) {
	for _, tc := range []struct {
		st   Status
		want bool
	}{
		{Status{Connected: true, Registered: true}, true},
		{Status{Connected: true}, false},
		{Status{Registered: true}, false},
		{Status{Connected: true, Registered: true, Terminated: true}, false},
		{Status{}, false},
	} {
		if got := tc.st.Online(); got != tc.want {
			t.Errorf("%+v: %v", tc.st, got)
		}
	}
}

func TestSortedPeersEdgeCases(t *testing.T) {
	st := &Status{Peers: map[int]Peer{
		7:  {Name: "same"},
		5:  {Name: "Same"},
		6:  {SiteID: 6, Name: "same"},
		9:  {Name: ""},
		2:  {Name: "Ünï"},
		11: {Name: "apple"},
	}}
	got := st.SortedPeers()
	var ids []int
	for _, p := range got {
		ids = append(ids, p.SiteID)
	}
	// Names are compared without case, and ties go to the lower site id.
	// The unnamed peer sorts first, and the ID is filled in from the key.
	if want := []int{9, 11, 5, 6, 7, 2}; !slices.Equal(ids, want) {
		t.Fatalf("order %v, want %v", ids, want)
	}
	for i := range 100 {
		again := st.SortedPeers()
		for j := range got {
			if again[j].SiteID != got[j].SiteID {
				t.Fatalf("run %d: order is not stable", i)
			}
		}
	}
	// Sorting must not touch the status itself.
	if st.Peers[7].SiteID != 0 {
		t.Fatal("SortedPeers modified the map")
	}
}

func TestPeerMode(t *testing.T) {
	for _, tc := range []struct {
		p    Peer
		want string
	}{
		{Peer{}, "Direct"},
		{Peer{IsRelay: true}, "Relay"},
		{Peer{IsLocal: true}, "Local"},
		{Peer{IsLocal: true, IsRelay: true}, "Local"},
	} {
		if got := tc.p.Mode(); got != tc.want {
			t.Errorf("%+v: %q", tc.p, got)
		}
	}
}

func TestTunnelIPForms(t *testing.T) {
	for name, tc := range map[string]struct {
		settings map[string]any
		want     string
	}{
		"nil":             {nil, ""},
		"empty":           {map[string]any{}, ""},
		"plain":           {map[string]any{"tunnelIp": "100.90.128.4"}, "100.90.128.4"},
		"with mask":       {map[string]any{"tunnelIp": "100.90.128.4/32"}, "100.90.128.4"},
		"ipv6":            {map[string]any{"address": "fd00::4/64"}, "fd00::4"},
		"list":            {map[string]any{"address": []any{"", "10.0.0.2/24", "10.0.0.3"}}, "10.0.0.2"},
		"list of numbers": {map[string]any{"address": []any{1, 2}}, ""},
		"number":          {map[string]any{"tunnelIp": 5}, ""},
		"empty string":    {map[string]any{"tunnelIp": "", "address": "10.0.0.9"}, "10.0.0.9"},
		"priority":        {map[string]any{"ip": "1.1.1.1", "tunnelIp": "2.2.2.2"}, "2.2.2.2"},
		"fuzzy key":       {map[string]any{"clientIPv4Address": "10.1.1.1/32"}, "10.1.1.1"},
		"nothing useful":  {map[string]any{"mtu": 1280, "dns": "1.1.1.1"}, ""},
	} {
		got := (&Status{NetworkSettings: tc.settings}).TunnelIP()
		if got != tc.want {
			t.Errorf("%s: %q, want %q", name, got, tc.want)
		}
	}
}

func TestTunnelIPIsDeterministic(t *testing.T) {
	// With several keys that look like an address, map iteration order must
	// not decide which one the UI shows.
	st := &Status{NetworkSettings: map[string]any{
		"gatewayIPv4Address": "10.9.9.9",
		"clientIPv4Address":  "10.1.1.1",
		"dnsIPv4Address":     "10.5.5.5",
		"peerIPv4Address":    "10.7.7.7",
	}}
	first := st.TunnelIP()
	for i := range 500 {
		if got := st.TunnelIP(); got != first {
			t.Fatalf("run %d: %q then %q", i, first, got)
		}
	}
}

func TestIsGateway(t *testing.T) {
	st := &Status{GatewayActive: true, GatewaySiteIDs: []int{1, 3}}
	if !st.IsGateway(1) || !st.IsGateway(3) || st.IsGateway(2) || st.IsGateway(0) {
		t.Fatal("membership")
	}
	st.GatewayActive = false
	if st.IsGateway(1) {
		t.Fatal("inactive gateway")
	}
}

func TestRunningIsFalseForEveryFailure(t *testing.T) {
	for name, h := range map[string]http.Handler{
		"500":  reply(500, ""),
		"404":  reply(404, ""),
		"hang": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }),
	} {
		c := serve(t, h)
		ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
		if c.Running(ctx) {
			t.Errorf("%s: reported running", name)
		}
		cancel()
	}
	if New(filepath.Join(t.TempDir(), "none")).Running(t.Context()) {
		t.Error("missing socket")
	}
}

func TestWaitStoppedHonorsTheContext(t *testing.T) {
	// A tunnel that never stops, with a health endpoint that sometimes hangs.
	// WaitStopped must not call a timed-out check "stopped".
	for name, h := range map[string]http.Handler{
		"healthy": reply(200, `{"status":"ok"}`),
		"hung": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
		}),
	} {
		t.Run(name, func(t *testing.T) {
			c := serve(t, h)
			ctx, cancel := context.WithTimeout(t.Context(), 400*time.Millisecond)
			defer cancel()
			start := time.Now()
			err := c.WaitStopped(ctx)
			if err == nil {
				t.Fatal("WaitStopped said the tunnel stopped, but it is still up")
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("err = %v", err)
			}
			if time.Since(start) > 3*time.Second {
				t.Errorf("took %v", time.Since(start))
			}
		})
	}
}

func TestWaitStoppedAlreadyStopped(t *testing.T) {
	c := New(filepath.Join(t.TempDir(), "none"))
	if err := c.WaitStopped(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestWaitStoppedWithCanceledContext(t *testing.T) {
	c := serve(t, reply(200, `{}`))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := c.WaitStopped(ctx); err == nil {
		t.Fatal("a canceled wait is not a successful stop")
	}
}

func TestCanceledContextIsNotNotRunning(t *testing.T) {
	c := serve(t, reply(200, `{}`))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := c.Status(ctx)
	if err == nil || errors.Is(err, ErrNotRunning) || errors.Is(err, ErrPermission) {
		t.Fatalf("%v", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("cancellation should be visible to callers: %v", err)
	}
}

func TestSlowServerTimesOut(t *testing.T) {
	if testing.Short() {
		t.Skip("takes four seconds")
	}
	c := serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
	}))
	start := time.Now()
	if _, err := c.Status(t.Context()); err == nil {
		t.Fatal("expected a timeout")
	}
	if d := time.Since(start); d > 6*time.Second {
		t.Fatalf("gave up after %v", d)
	}
}

func TestSocketPermissionDenied(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can open everything")
	}
	dir := t.TempDir()
	sock := filepath.Join(dir, "olm.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o700)
	c := New(sock)
	if _, err := c.Status(t.Context()); !errors.Is(err, ErrPermission) {
		t.Errorf("status: %v", err)
	}
	// A tunnel behind a socket we may not open could still be up, and
	// callers such as logout must not skip stopping it.
	if !c.Running(t.Context()) {
		t.Error("a socket we may not open was reported as no tunnel")
	}
	if err := c.Exit(t.Context()); !errors.Is(err, ErrPermission) {
		t.Errorf("exit: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if err := c.WaitStopped(ctx); !errors.Is(err, ErrPermission) {
		t.Errorf("wait stopped: %v, want ErrPermission rather than a claim that it stopped", err)
	}
}

func TestSocketNotWritableByUs(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can connect to everything")
	}
	dir := t.TempDir()
	sock := filepath.Join(dir, "olm.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if err := os.Chmod(sock, 0o400); err != nil { // connect needs write
		t.Fatal(err)
	}
	if _, err := New(sock).Status(t.Context()); !errors.Is(err, ErrPermission) {
		t.Errorf("%v", err)
	}
}

func TestSocketPathIsAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "olm.sock")
	if err := os.WriteFile(path, []byte("not a socket"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := New(path)
	_, err := c.Status(t.Context())
	if err == nil {
		t.Fatal("a regular file is not an olm socket")
	}
	if c.Running(t.Context()) {
		t.Fatal("not running")
	}
}

func TestRequestsCarryTheRightMethodPathAndBody(t *testing.T) {
	type seen struct{ method, path, ctype, body string }
	var mu sync.Mutex
	var log []seen
	c := serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		log = append(log, seen{r.Method, r.URL.Path, r.Header.Get("Content-Type"), string(b)})
		mu.Unlock()
		if r.URL.Path == "/gateway/select" {
			w.WriteHeader(http.StatusAccepted)
		}
		io.WriteString(w, `{}`)
	}))
	ctx := t.Context()
	if err := c.Exit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.DisableGateway(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.SwitchOrg(ctx, `we"ird\org`); err != nil {
		t.Fatal(err)
	}
	if err := c.SelectGateway(ctx, 12, []int{3, 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Status(ctx); err != nil {
		t.Fatal(err)
	}
	want := []seen{
		{"POST", "/exit", "", ""},
		{"POST", "/gateway/disable", "", ""},
		{"POST", "/switch-org", "application/json", `{"org_id":"we\"ird\\org"}`},
		{"POST", "/gateway/select", "application/json", `{"siteIds":[3,1],"siteResourceId":12}`},
		{"GET", "/status", "", ""},
	}
	if !slices.Equal(log, want) {
		t.Fatalf("requests\n got %+v\nwant %+v", log, want)
	}
	var sw map[string]string
	if err := json.Unmarshal([]byte(log[2].body), &sw); err != nil || sw["org_id"] != `we"ird\org` {
		t.Errorf("org id did not survive: %v %v", sw, err)
	}
}

func TestInputValidationNeverReachesTheSocket(t *testing.T) {
	var hits atomic.Int64
	c := serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	ctx := t.Context()
	for name, err := range map[string]error{
		"empty org":         c.SwitchOrg(ctx, ""),
		"zero resource":     c.SelectGateway(ctx, 0, []int{1}),
		"negative resource": c.SelectGateway(ctx, -3, []int{1}),
		"no sites":          c.SelectGateway(ctx, 1, nil),
		"empty sites":       c.SelectGateway(ctx, 1, []int{}),
	} {
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if hits.Load() != 0 {
		t.Errorf("%d invalid requests were sent", hits.Load())
	}
}

func TestSelectGatewayAcceptsOKAndAccepted(t *testing.T) {
	for _, code := range []int{200, 202} {
		c := serve(t, reply(code, `{}`))
		if err := c.SelectGateway(t.Context(), 1, []int{1}); err != nil {
			t.Errorf("%d: %v", code, err)
		}
	}
	for _, code := range []int{400, 404, 409, 500, 503} {
		c := serve(t, reply(code, `{"error":"nope"}`))
		if err := c.SelectGateway(t.Context(), 1, []int{1}); err == nil {
			t.Errorf("%d: accepted", code)
		}
	}
}

func openFDs(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Skip("no /proc")
	}
	return len(entries)
}

// TestManyRequestsDoNotLeak makes a few thousand calls, including failing
// ones, and checks that no file descriptors are left behind. The tray polls
// the socket every few seconds for as long as it runs.
func TestManyRequestsDoNotLeak(t *testing.T) {
	c := serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/status" && r.URL.Query().Get("fail") == "" {
			io.WriteString(w, statusJSON)
			return
		}
		w.WriteHeader(500)
	}))
	// Warm up so lazily opened descriptors are counted in the baseline.
	for range 5 {
		c.Status(t.Context())
		c.Running(t.Context())
	}
	before := openFDs(t)
	for i := range 1500 {
		switch i % 3 {
		case 0:
			if _, err := c.Status(t.Context()); err != nil {
				t.Fatal(err)
			}
		case 1:
			c.Running(t.Context())
		case 2:
			c.Exit(t.Context()) // answered with 500
		}
	}
	time.Sleep(100 * time.Millisecond)
	if after := openFDs(t); after > before+8 {
		t.Fatalf("file descriptors grew from %d to %d", before, after)
	}
}

func TestConcurrentCalls(t *testing.T) {
	c := serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/status":
			io.WriteString(w, statusJSON)
		default:
			io.WriteString(w, `{"status":"ok"}`)
		}
	}))
	var wg sync.WaitGroup
	var failures atomic.Int64
	for i := range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 25 {
				switch i % 4 {
				case 0:
					st, err := c.Status(t.Context())
					if err != nil || st.TunnelIP() != "100.90.128.4" || len(st.SortedPeers()) != 2 {
						failures.Add(1)
					}
				case 1:
					if !c.Running(t.Context()) {
						failures.Add(1)
					}
				case 2:
					if err := c.SwitchOrg(t.Context(), fmt.Sprintf("org-%d", i)); err != nil {
						failures.Add(1)
					}
				case 3:
					if err := c.SelectGateway(t.Context(), i+1, []int{1, 2}); err != nil {
						failures.Add(1)
					}
				}
			}
		}()
	}
	wg.Wait()
	if n := failures.Load(); n > 0 {
		t.Fatalf("%d of 1600 calls failed", n)
	}
}

func TestServerDisappearsMidFlight(t *testing.T) {
	// olm exits while the tray is polling. Every call has to end with either
	// a result or one of the documented errors, never a hang or a panic.
	dir, err := os.MkdirTemp("", "olm")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "olm.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Millisecond)
		io.WriteString(w, statusJSON)
	})}
	go srv.Serve(ln)
	c := New(sock)

	var wg sync.WaitGroup
	done := make(chan struct{})
	var bad atomic.Int64
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
				_, err := c.Status(ctx)
				cancel()
				if err != nil && !errors.Is(err, ErrNotRunning) && !strings.Contains(err.Error(), "olm GET /status") {
					bad.Add(1)
					t.Errorf("unexpected error: %v", err)
				}
			}
		}()
	}
	time.Sleep(100 * time.Millisecond)
	srv.Close()
	os.Remove(sock)
	time.Sleep(100 * time.Millisecond)
	close(done)
	wg.Wait()
	if _, err := c.Status(t.Context()); !errors.Is(err, ErrNotRunning) {
		t.Errorf("after the socket is gone: %v", err)
	}
}

func TestStaleSocketFileIsNotRunningEvenForPost(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "olm.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	ln.Close()
	c := New(sock)
	ctx := t.Context()
	for name, err := range map[string]error{
		"exit":    c.Exit(ctx),
		"switch":  c.SwitchOrg(ctx, "o"),
		"gateway": c.SelectGateway(ctx, 1, []int{1}),
		"disable": c.DisableGateway(ctx),
	} {
		if !errors.Is(err, ErrNotRunning) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestWaitStoppedKeepsWaitingOnAnUnhealthyTunnel(t *testing.T) {
	// A tunnel that answers with an error is still a tunnel. Calling it
	// stopped would tell the user they are disconnected while traffic still
	// flows through it.
	for name, code := range map[string]int{"500": 500, "404": 404, "503": 503} {
		t.Run(name, func(t *testing.T) {
			c := serve(t, reply(code, "busy"))
			ctx, cancel := context.WithTimeout(t.Context(), 350*time.Millisecond)
			defer cancel()
			if err := c.WaitStopped(ctx); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("err = %v, want to keep waiting until the deadline", err)
			}
		})
	}
}

func TestWaitStoppedThroughAShutdown(t *testing.T) {
	// The tunnel answers a few checks, then drops connections while it
	// shuts down, then removes its socket. Only the last means stopped.
	dir, err := os.MkdirTemp("", "olm")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "olm.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	stopped := make(chan struct{})
	var once sync.Once
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		switch {
		case n <= 2:
			io.WriteString(w, `{}`)
		case n <= 5:
			// Dropped mid-request.
			if hj, ok := w.(http.Hijacker); ok {
				conn, _, _ := hj.Hijack()
				conn.Close()
			}
		default:
			once.Do(func() { close(stopped) })
		}
	})}
	go srv.Serve(ln)
	go func() {
		<-stopped
		srv.Close()
	}()
	t.Cleanup(func() { srv.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := New(sock).WaitStopped(ctx); err != nil {
		t.Fatalf("WaitStopped: %v", err)
	}
	if n := calls.Load(); n < 6 {
		t.Fatalf("returned after %d checks, before the tunnel was gone", n)
	}
}

func TestWaitStoppedWithStaleSocketFile(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "olm.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	ln.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if err := New(sock).WaitStopped(ctx); err != nil {
		t.Fatalf("a socket file nobody listens on is a stopped tunnel: %v", err)
	}
}

func FuzzStatusDecode(f *testing.F) {
	f.Add([]byte(statusJSON))
	f.Add([]byte(`{"peers":{"1":{}},"networkSettings":{"a":[null,1,"x"]},"exitNode":{}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var st Status
		if json.Unmarshal(data, &st) != nil {
			return
		}
		// Whatever decodes must be safe to use.
		_ = st.Online()
		_ = st.TunnelIP()
		for _, p := range st.SortedPeers() {
			_ = p.Mode()
		}
		_ = st.IsGateway(1)
	})
}
