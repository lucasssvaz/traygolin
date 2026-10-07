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
	"errors"
	"reflect"

	"github.com/lucasssvaz/traygolin/internal/olm"
	"github.com/lucasssvaz/traygolin/internal/pangolin"
	"github.com/lucasssvaz/traygolin/internal/server"
)

// Status is one of *TunnelStatus, *AccountStatus, *ServerStatus or
// *CLIStatus.
type Status interface {
	status()
}

// TunnelStatus is the state of the root tunnel process.
type TunnelStatus struct {
	// Running is true when the olm socket exists and answers, or exists
	// but is not readable by this user.
	Running bool
	// Status is nil when the tunnel is not running or unreadable.
	Status *olm.Status
	Err    error
}

func (*TunnelStatus) status() {}

// Online reports whether the tunnel is connected and registered.
func (s *TunnelStatus) Online() bool {
	return s != nil && s.Status != nil && s.Status.Online()
}

// Connecting reports whether the tunnel process is up but not yet online.
func (s *TunnelStatus) Connecting() bool {
	return s != nil && s.Running && s.Status != nil && !s.Status.Online() && !s.Status.Terminated
}

// Unreadable reports whether the socket exists but this user cannot use it.
func (s *TunnelStatus) Unreadable() bool {
	return s != nil && errors.Is(s.Err, olm.ErrPermission)
}

// ExitNodeActive reports whether all traffic goes through an exit node.
func (s *TunnelStatus) ExitNodeActive() bool {
	return s.Online() && s.Status.GatewayActive
}

// Peers returns the sites in display order.
func (s *TunnelStatus) Peers() []olm.Peer {
	if s == nil || s.Status == nil {
		return nil
	}
	return s.Status.SortedPeers()
}

// AccountStatus is the CLI's accounts.json and config.json.
type AccountStatus struct {
	Auth   pangolin.Auth
	Config pangolin.CLIConfig
}

func (*AccountStatus) status() {}

func (s *AccountStatus) equal(o *AccountStatus) bool {
	return reflect.DeepEqual(s.Auth, o.Auth) && s.Config.Equal(o.Config)
}

// ServerStatus is what the Pangolin server reports for the active
// account. It is empty when nobody is logged in.
type ServerStatus struct {
	UserID string
	OrgID  string
	// Info is the server's version and build, live when it could be
	// fetched and otherwise as saved at login.
	Info      server.Info
	Orgs      []server.Org
	ExitNodes []server.ExitNode
	Err       error
}

func (*ServerStatus) status() {}

func (s *ServerStatus) equal(o *ServerStatus) bool {
	if s == nil || o == nil {
		return s == o
	}
	errText := func(err error) string {
		if err == nil {
			return ""
		}
		return err.Error()
	}
	return s.UserID == o.UserID && s.OrgID == o.OrgID && s.Info == o.Info &&
		reflect.DeepEqual(s.Orgs, o.Orgs) && reflect.DeepEqual(s.ExitNodes, o.ExitNodes) &&
		errText(s.Err) == errText(o.Err)
}

// Org returns the organization with id, if the server listed it.
func (s *ServerStatus) Org(id string) (server.Org, bool) {
	if s != nil {
		for _, o := range s.Orgs {
			if o.ID == id {
				return o, true
			}
		}
	}
	return server.Org{}, false
}

// ExitNode returns the exit node with resourceID, if the server listed it.
func (s *ServerStatus) ExitNode(resourceID int) (server.ExitNode, bool) {
	if s != nil && resourceID != 0 {
		for _, e := range s.ExitNodes {
			if e.ResourceID == resourceID {
				return e, true
			}
		}
	}
	return server.ExitNode{}, false
}

// CLIStatus describes the installed CLI and helper.
type CLIStatus struct {
	Path            string
	Version         string
	Err             error
	HelperInstalled bool
}

func (*CLIStatus) status() {}

// Found reports whether the CLI binary is on PATH.
func (s *CLIStatus) Found() bool { return s != nil && s.Err == nil && s.Path != "" }

func (s *CLIStatus) equal(o *CLIStatus) bool {
	return s.Path == o.Path && s.Version == o.Version && s.HelperInstalled == o.HelperInstalled && (s.Err == nil) == (o.Err == nil)
}
