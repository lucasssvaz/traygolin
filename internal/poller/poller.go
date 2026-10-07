// Copyright 2026 Lucas Saavedra Vaz
// Portions Copyright (c) 2025 DeedleFake, MIT License; see LICENSES/MIT-Trayscale.txt
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

// Package poller fetches tunnel, account and CLI state at regular
// intervals or when triggered. Its channel design is adapted from
// Trayscale's tsutil.Poller.
package poller

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/lucasssvaz/traygolin/internal/olm"
	"github.com/lucasssvaz/traygolin/internal/pangolin"
	"github.com/lucasssvaz/traygolin/internal/privhelper"
	"github.com/lucasssvaz/traygolin/internal/server"
)

const (
	DefaultInterval = 5 * time.Second
	FastInterval    = time.Second
	MinInterval     = time.Second
	MaxInterval     = time.Hour

	// ServerInterval is how often organizations and exit nodes are
	// refetched when the account and organization have not changed.
	ServerInterval = time.Minute
	// ServerRetry is how soon a failed server fetch is retried.
	ServerRetry = 15 * time.Second
)

// ServerAPI is the subset of [server.Client] the poller uses.
type ServerAPI interface {
	Info(ctx context.Context) (server.Info, error)
	Orgs(ctx context.Context, userID string) ([]server.Org, error)
	ExitNodes(ctx context.Context, orgID string) ([]server.ExitNode, error)
}

// ClampInterval keeps d within [MinInterval, MaxInterval].
func ClampInterval(d time.Duration) time.Duration {
	return min(max(d, MinInterval), MaxInterval)
}

// A Poller gets the latest Pangolin state at regular intervals or when
// manually triggered.
//
// It is a race condition to change any exported fields of Poller while
// Run is running.
type Poller struct {
	// Interval is the default interval to use for polling.
	Interval time.Duration

	// If non-nil, New will be called when a new status is fetched.
	New func(Status)

	Olm *olm.Client
	CLI *pangolin.Client

	// Server returns an API client for a saved account. It defaults to
	// [pangolin.ServerClient].
	Server func(userID string) (ServerAPI, error)

	once sync.Once

	poll       chan struct{}
	getTunnel  chan *TunnelStatus
	nextTunnel chan *TunnelStatus
	interval   chan time.Duration
	fast       chan time.Duration
}

func (p *Poller) init() {
	p.once.Do(func() {
		p.poll = make(chan struct{})
		p.getTunnel = make(chan *TunnelStatus)
		p.nextTunnel = make(chan *TunnelStatus)
		p.interval = make(chan time.Duration)
		p.fast = make(chan time.Duration)
		if p.Olm == nil {
			p.Olm = olm.New("")
		}
		if p.CLI == nil {
			p.CLI = &pangolin.Client{}
		}
		if p.Server == nil {
			p.Server = func(userID string) (ServerAPI, error) {
				c, err := pangolin.ServerClient(userID)
				if err != nil {
					return nil, err
				}
				return c, nil
			}
		}
	})
}

// Run runs the poller. It blocks until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) {
	p.init()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	n := newNotifier()
	go p.watchTunnel(ctx, n)
	go p.watchAccounts(ctx, n)
	go p.watchServer(ctx, n)
	go p.watchCLI(ctx, n)

	interval := p.Interval
	if interval <= 0 {
		interval = DefaultInterval
	}
	var fastUntil time.Time
	current := func() time.Duration {
		if time.Now().Before(fastUntil) {
			return min(FastInterval, interval)
		}
		return interval
	}

	check := time.NewTimer(current())
	defer check.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case p.poll <- struct{}{}:
			n = n.Notify()
		case interval = <-p.interval:
			n = n.Notify()
		case d := <-p.fast:
			fastUntil = time.Now().Add(d)
			n = n.Notify()
		case <-check.C:
			n = n.Notify()
		}
		check.Reset(current())
	}
}

func (p *Poller) publisher(ctx context.Context) chan<- *TunnelStatus {
	set := make(chan *TunnelStatus)
	go func() {
		var get chan *TunnelStatus
		var s *TunnelStatus
		for {
			select {
			case <-ctx.Done():
				return
			case s = <-set:
				get = p.getTunnel
				if p.New != nil {
					p.New(s)
				}
			case get <- s:
			}
		}
	}()
	return set
}

func (p *Poller) watchTunnel(ctx context.Context, n *notifier) {
	set := p.publisher(ctx)
	for {
		s := p.fetchTunnel(ctx)
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case set <- s:
		}
		select {
		case p.nextTunnel <- s:
		default:
		}

		select {
		case <-ctx.Done():
			return
		case <-n.notify:
			n = n.next
		}
	}
}

func (p *Poller) fetchTunnel(ctx context.Context) *TunnelStatus {
	st, err := p.Olm.Status(ctx)
	switch {
	case err == nil:
		return &TunnelStatus{Running: true, Status: st}
	case errors.Is(err, olm.ErrNotRunning):
		return &TunnelStatus{}
	case errors.Is(err, olm.ErrPermission):
		return &TunnelStatus{Running: true, Err: err}
	default:
		if ctx.Err() == nil {
			slog.Debug("get tunnel status", "err", err)
		}
		return &TunnelStatus{Err: err}
	}
}

func (p *Poller) watchAccounts(ctx context.Context, n *notifier) {
	var prev *AccountStatus
	for {
		s := &AccountStatus{Auth: pangolin.LoadAuth(), Config: pangolin.LoadConfig()}
		if prev == nil || !prev.equal(s) {
			prev = s
			if p.New != nil {
				p.New(s)
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-n.notify:
			n = n.next
		}
	}
}

// watchServer fetches organizations and exit nodes when the active
// account or organization changes, and every ServerInterval otherwise.
func (p *Poller) watchServer(ctx context.Context, n *notifier) {
	var prev *ServerStatus
	var key string
	var last time.Time
	for {
		acc := pangolin.LoadAuth().Active
		k := acc.UserID + "\x00" + acc.Host + "\x00" + acc.OrgID
		due := ServerInterval
		if prev != nil && prev.Err != nil {
			due = ServerRetry
		}
		if prev == nil || k != key || time.Since(last) >= due {
			s := p.fetchServer(ctx, acc)
			if ctx.Err() != nil {
				return
			}
			if s.Err != nil && prev != nil && k == key {
				s.Orgs, s.ExitNodes = prev.Orgs, prev.ExitNodes
				if prev.Info.Version != "" {
					s.Info = prev.Info
				}
			}
			key, last = k, time.Now()
			if !s.equal(prev) {
				prev = s
				if p.New != nil {
					p.New(s)
				}
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-n.notify:
			n = n.next
		}
	}
}

func (p *Poller) fetchServer(ctx context.Context, acc pangolin.Account) *ServerStatus {
	s := &ServerStatus{UserID: acc.UserID, OrgID: acc.OrgID, Info: acc.Server}
	if acc.UserID == "" {
		return s
	}
	api, err := p.Server(acc.UserID)
	if err != nil {
		s.Err = err
		return s
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if info, err := api.Info(ctx); err == nil && info.Version != "" {
		s.Info = info
	}
	var errs []error
	if s.Orgs, err = api.Orgs(ctx, acc.UserID); err != nil {
		errs = append(errs, err)
	}
	if acc.OrgID != "" {
		if s.ExitNodes, err = api.ExitNodes(ctx, acc.OrgID); err != nil {
			errs = append(errs, err)
		}
	}
	if s.Err = errors.Join(errs...); s.Err != nil && ctx.Err() == nil {
		slog.Warn("fetch from Pangolin server", "err", s.Err)
	}
	return s
}

func (p *Poller) watchCLI(ctx context.Context, n *notifier) {
	var prev *CLIStatus
	for {
		s := &CLIStatus{HelperInstalled: privhelper.Ready()}
		s.Path, s.Err = p.CLI.LookPath()
		if prev != nil && prev.Path == s.Path {
			s.Version = prev.Version
		} else if s.Err == nil {
			v, err := p.CLI.Version(ctx)
			if err != nil {
				slog.Warn("get CLI version", "err", err)
			}
			s.Version = v
		}
		if prev == nil || !prev.equal(s) {
			prev = s
			if p.New != nil {
				p.New(s)
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-n.notify:
			n = n.next
		}
	}
}

// Poll returns a channel that, when received from, refreshes all state.
func (p *Poller) Poll() <-chan struct{} {
	p.init()
	return p.poll
}

// GetTunnel returns a channel that yields the most recently fetched
// tunnel status. It blocks until the status has been fetched once.
func (p *Poller) GetTunnel() <-chan *TunnelStatus {
	p.init()
	return p.getTunnel
}

// NextTunnel returns a channel that is sent each new TunnelStatus if
// anyone is receiving from it. Unlike [GetTunnel], it does not yield the
// previous status, so it is useful when an update is expected soon.
func (p *Poller) NextTunnel() <-chan *TunnelStatus {
	p.init()
	return p.nextTunnel
}

// SetInterval returns a channel that modifies the polling interval of a
// running poller.
func (p *Poller) SetInterval() chan<- time.Duration {
	p.init()
	return p.interval
}

// Fast returns a channel that switches to [FastInterval] polling for the
// duration sent on it. It is used while connecting or disconnecting.
func (p *Poller) Fast() chan<- time.Duration {
	p.init()
	return p.fast
}

type notifier struct {
	notify chan struct{}
	next   *notifier
}

func newNotifier() *notifier {
	return &notifier{notify: make(chan struct{})}
}

func (n *notifier) Notify() *notifier {
	n.next = newNotifier()
	close(n.notify)
	return n.next
}
