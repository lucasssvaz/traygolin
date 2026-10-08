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
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lucasssvaz/traygolin/internal/olm"
	"github.com/lucasssvaz/traygolin/internal/pangolin"
	"github.com/lucasssvaz/traygolin/internal/server"
)

func fakeOlm(t *testing.T, online *atomic.Bool) string {
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
	mux := http.NewServeMux()
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		if online.Load() {
			w.Write([]byte(`{"connected":true,"registered":true,"orgId":"org","gatewayActive":true,"peers":{"1":{"siteId":1,"name":"Office","connected":true}}}`))
			return
		}
		w.Write([]byte(`{"connected":false,"registered":false}`))
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return sock
}

func fakeCLI(t *testing.T) *pangolin.Client {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "pangolin")
	os.WriteFile(bin, []byte("#!/bin/sh\necho 0.18.1\n"), 0o755)
	return &pangolin.Client{Binary: bin}
}

func TestPollerPublishesAllKinds(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var online atomic.Bool
	online.Store(true)
	got := make(chan Status, 16)
	p := &Poller{
		Interval: time.Hour,
		Olm:      olm.New(fakeOlm(t, &online)),
		CLI:      fakeCLI(t),
		New:      func(s Status) { got <- s },
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	go p.Run(ctx)

	var tun *TunnelStatus
	var acc *AccountStatus
	var cli *CLIStatus
	for tun == nil || acc == nil || cli == nil {
		select {
		case s := <-got:
			switch s := s.(type) {
			case *TunnelStatus:
				tun = s
			case *AccountStatus:
				acc = s
			case *CLIStatus:
				cli = s
			}
		case <-ctx.Done():
			t.Fatalf("timeout tun=%v acc=%v cli=%v", tun, acc, cli)
		}
	}
	if !tun.Online() || !tun.ExitNodeActive() || len(tun.Peers()) != 1 {
		t.Fatalf("%+v", tun)
	}
	if acc.Auth.LoggedIn {
		t.Fatal("no accounts file")
	}
	if !cli.Found() || cli.Version != "0.18.1" {
		t.Fatalf("%+v", cli)
	}

	select {
	case s := <-p.GetTunnel():
		if !s.Online() {
			t.Fatal("GetTunnel")
		}
	case <-ctx.Done():
		t.Fatal("GetTunnel blocked")
	}

	online.Store(false)
	<-p.Poll()
	select {
	case s := <-p.NextTunnel():
		if s.Online() || !s.Running {
			t.Fatalf("after poll %+v", s)
		}
	case <-ctx.Done():
		t.Fatal("NextTunnel blocked")
	}
}

func TestPollerNotRunningAndFast(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	p := &Poller{
		Interval: time.Hour,
		Olm:      olm.New(filepath.Join(t.TempDir(), "missing.sock")),
		CLI:      &pangolin.Client{Binary: "/does/not/exist"},
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	go p.Run(ctx)
	s := <-p.GetTunnel()
	if s.Running || s.Online() || s.Err != nil {
		t.Fatalf("%+v", s)
	}
	p.Fast() <- 3 * time.Second
	start := time.Now()
	<-p.NextTunnel()
	<-p.NextTunnel()
	if time.Since(start) > 2500*time.Millisecond {
		t.Fatal("fast polling did not speed up")
	}
}

type fakeServerAPI struct {
	fail     atomic.Bool
	exitOrgs chan string
}

func (f *fakeServerAPI) Info(ctx context.Context) (server.Info, error) {
	if f.fail.Load() {
		return server.Info{}, errors.New("offline")
	}
	return server.Info{Version: "1.24.1", Build: "oss"}, nil
}

func (f *fakeServerAPI) Orgs(ctx context.Context, userID string) ([]server.Org, error) {
	if f.fail.Load() {
		return nil, errors.New("offline")
	}
	return []server.Org{{ID: "home", Name: "Home"}, {ID: "work", Name: "Work"}}, nil
}

func (f *fakeServerAPI) ExitNodes(ctx context.Context, orgID string) ([]server.ExitNode, error) {
	f.exitOrgs <- orgID
	if f.fail.Load() {
		return nil, errors.New("offline")
	}
	return []server.ExitNode{{ResourceID: 7, NiceID: orgID + "-exit", SiteIDs: []int{1}}}, nil
}

func writeAccounts(t *testing.T, org string) {
	t.Helper()
	dir := pangolin.ConfigDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	data := `{"activeuserid":"u1","accounts":{"u1":{"userId":"u1","host":"https://p.example","email":"me@example.com","sessionToken":"tok","orgId":"` + org + `","serverInfo":{"version":"1.21.1","build":"enterprise"}}}}`
	if err := os.WriteFile(filepath.Join(dir, "accounts.json"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPollerServer(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeAccounts(t, "home")
	api := &fakeServerAPI{exitOrgs: make(chan string, 16)}
	got := make(chan *ServerStatus, 16)
	p := &Poller{
		Interval: time.Hour,
		Olm:      olm.New(filepath.Join(t.TempDir(), "missing.sock")),
		CLI:      &pangolin.Client{Binary: "/does/not/exist"},
		Server:   func(string) (ServerAPI, error) { return api, nil },
		New: func(s Status) {
			if s, ok := s.(*ServerStatus); ok {
				got <- s
			}
		},
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	go p.Run(ctx)

	next := func() *ServerStatus {
		t.Helper()
		select {
		case s := <-got:
			return s
		case <-ctx.Done():
			t.Fatal("no server status")
			return nil
		}
	}
	s := next()
	if s.OrgID != "home" || len(s.Orgs) != 2 || len(s.ExitNodes) != 1 || s.ExitNodes[0].NiceID != "home-exit" || s.Err != nil {
		t.Fatalf("%+v", s)
	}
	if s.Info != (server.Info{Version: "1.24.1", Build: "oss"}) {
		t.Fatalf("live server info %+v", s.Info)
	}
	if <-api.exitOrgs != "home" {
		t.Fatal("exit nodes for the active org")
	}

	// Polling again within ServerInterval does not refetch.
	<-p.Poll()
	<-p.Poll()
	select {
	case org := <-api.exitOrgs:
		t.Fatalf("unexpected refetch for %s", org)
	case <-time.After(200 * time.Millisecond):
	}

	// Switching organization refetches right away.
	writeAccounts(t, "work")
	<-p.Poll()
	if s := next(); s.OrgID != "work" || s.ExitNodes[0].NiceID != "work-exit" {
		t.Fatalf("after switch %+v", s)
	}

	// A failed fetch is reported, not dropped.
	api.fail.Store(true)
	writeAccounts(t, "home")
	<-p.Poll()
	s = next()
	if s.Err == nil || s.OrgID != "home" {
		t.Fatalf("failure %+v", s)
	}
	if s.Info.Version != "1.21.1" {
		t.Fatalf("saved server info fallback %+v", s.Info)
	}
}

func TestServerStatusLookup(t *testing.T) {
	s := &ServerStatus{
		Orgs:      []server.Org{{ID: "home", Name: "Home"}},
		ExitNodes: []server.ExitNode{{ResourceID: 7, NiceID: "us"}},
	}
	if o, ok := s.Org("home"); !ok || o.Name != "Home" {
		t.Fatal("org lookup")
	}
	if _, ok := s.ExitNode(0); ok {
		t.Fatal("0 is no exit node")
	}
	if e, ok := s.ExitNode(7); !ok || e.NiceID != "us" {
		t.Fatal("exit node lookup")
	}
	var nilStatus *ServerStatus
	if _, ok := nilStatus.Org("home"); ok {
		t.Fatal("nil status")
	}
}

func TestClampInterval(t *testing.T) {
	if ClampInterval(0) != MinInterval || ClampInterval(2*time.Hour) != MaxInterval || ClampInterval(5*time.Second) != 5*time.Second {
		t.Fatal("clamp")
	}
}
