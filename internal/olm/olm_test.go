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
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type fakeOlm struct {
	mu       sync.Mutex
	exited   bool
	org      string
	gateway  map[string]any
	disabled bool
	srv      *http.Server
	ln       net.Listener
}

const statusJSON = `{
  "connected": true,
  "registered": true,
  "version": "1.2.3",
  "agent": "Pangolin CLI",
  "orgId": "org-a",
  "peers": {
    "3": {"siteId": 3, "name": "beta", "connected": true, "rtt": 17562199, "endpoint": "1.2.3.4:51820", "isRelay": false, "isLocal": false, "peerAddress": "100.90.128.3"},
    "1": {"siteId": 1, "name": "Alpha", "connected": true, "rtt": 2601796966, "endpoint": "5.6.7.8:51820", "isRelay": true, "isLocal": false}
  },
  "networkSettings": {"tunnelIp": "100.90.128.4"},
  "gatewayActive": true,
  "gatewaySiteIds": [1],
  "gatewaySiteResourceId": 9
}`

func startFake(t *testing.T) (*fakeOlm, string) {
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
	f := &fakeOlm{ln: ln}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(statusJSON))
	})
	mux.HandleFunc("POST /exit", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.exited = true
		f.mu.Unlock()
		w.Write([]byte(`{"status":"shutdown initiated"}`))
		go func() {
			time.Sleep(150 * time.Millisecond)
			f.srv.Close()
			os.Remove(sock)
		}()
	})
	mux.HandleFunc("POST /switch-org", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.org = body["org_id"]
		f.mu.Unlock()
		w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("POST /gateway/select", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.gateway = body
		f.mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"status":"accepted"}`))
	})
	mux.HandleFunc("POST /gateway/disable", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.disabled = true
		f.mu.Unlock()
		w.Write([]byte(`{"status":"ok"}`))
	})
	f.srv = &http.Server{Handler: mux}
	go f.srv.Serve(ln)
	t.Cleanup(func() { f.srv.Close() })
	return f, sock
}

func TestStatusDecode(t *testing.T) {
	_, sock := startFake(t)
	c := New(sock)
	st, err := c.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !st.Online() || st.OrgID != "org-a" || st.Agent != CLIAgent {
		t.Fatalf("%+v", st)
	}
	peers := st.SortedPeers()
	if len(peers) != 2 || peers[0].Name != "Alpha" || peers[1].Name != "beta" {
		t.Fatalf("order %+v", peers)
	}
	if peers[0].Mode() != "Relay" || peers[1].Mode() != "Direct" {
		t.Fatalf("modes %q %q", peers[0].Mode(), peers[1].Mode())
	}
	if peers[1].RTT != 17562199*time.Nanosecond {
		t.Fatalf("rtt %v", peers[1].RTT)
	}
	if !st.IsGateway(1) || st.IsGateway(3) || st.GatewayResource != 9 {
		t.Fatalf("gateway %+v", st)
	}
	if st.TunnelIP() != "100.90.128.4" {
		t.Fatalf("tunnel ip %q", st.TunnelIP())
	}
}

func TestExitAndWaitStopped(t *testing.T) {
	f, sock := startFake(t)
	c := New(sock)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if !c.Running(ctx) {
		t.Fatal("should be running")
	}
	if err := c.Exit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.WaitStopped(ctx); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.exited {
		t.Fatal("exit not received")
	}
	if _, err := c.Status(ctx); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("after exit: %v", err)
	}
}

func TestSwitchOrgAndGateway(t *testing.T) {
	f, sock := startFake(t)
	c := New(sock)
	ctx := context.Background()
	if err := c.SwitchOrg(ctx, "org-b"); err != nil {
		t.Fatal(err)
	}
	if err := c.SelectGateway(ctx, 9, []int{1, 3}); err != nil {
		t.Fatal(err)
	}
	if err := c.DisableGateway(ctx); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.org != "org-b" || !f.disabled {
		t.Fatalf("org %q disabled %v", f.org, f.disabled)
	}
	if f.gateway["siteResourceId"].(float64) != 9 || len(f.gateway["siteIds"].([]any)) != 2 {
		t.Fatalf("gateway %+v", f.gateway)
	}
	if err := c.SwitchOrg(ctx, ""); err == nil {
		t.Fatal("empty org should fail")
	}
	if err := c.SelectGateway(ctx, 0, nil); err == nil {
		t.Fatal("empty gateway should fail")
	}
}

func TestMissingSocket(t *testing.T) {
	c := New(filepath.Join(t.TempDir(), "nope.sock"))
	ctx := context.Background()
	if c.Running(ctx) {
		t.Fatal("should not run")
	}
	if _, err := c.Status(ctx); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("%v", err)
	}
	if err := c.Exit(ctx); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("%v", err)
	}
}

func TestStaleSocketIsNotRunning(t *testing.T) {
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
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	ln.Close()
	c := New(sock)
	if _, err := c.Status(context.Background()); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("%v", err)
	}
}
