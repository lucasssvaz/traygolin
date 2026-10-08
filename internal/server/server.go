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

// Package server reads organizations and exit nodes from a Pangolin
// server's HTTP API with the CLI's saved session. It only makes read
// requests; changes go through the CLI. It is an independent
// implementation and contains no code from the Pangolin CLI.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultCookieName is the session cookie Pangolin servers expect unless
// the CLI is configured otherwise.
const DefaultCookieName = "p_session_token"

// csrfToken is the fixed value Pangolin's API accepts from non-browser
// clients.
const csrfToken = "x-csrf-protection"

const pageSize = 100

// ErrUnauthorized means the saved session was rejected; the user has to
// log in again.
var ErrUnauthorized = errors.New("the Pangolin session has expired; log in again")

// Org is an organization the user belongs to.
type Org struct {
	ID   string
	Name string
}

// Label is the name shown in menus.
func (o Org) Label() string {
	if o.Name != "" {
		return o.Name
	}
	return o.ID
}

// ExitNode is an exit node resource and the sites behind it.
type ExitNode struct {
	ResourceID int
	NiceID     string
	Name       string
	SiteIDs    []int
	SiteNames  []string
	SiteOnline []bool
}

// Label is the name shown in menus.
func (e ExitNode) Label() string {
	if e.Name != "" {
		return e.Name
	}
	return e.NiceID
}

// Online reports whether any site behind the exit node is online. Servers
// that do not report site state are treated as online.
func (e ExitNode) Online() bool {
	if len(e.SiteOnline) == 0 {
		return true
	}
	for _, on := range e.SiteOnline {
		if on {
			return true
		}
	}
	return false
}

// Info describes the server software.
type Info struct {
	Version string
	// Build is "oss", "enterprise" or "saas".
	Build string
}

// String is how the official clients name the server, such as
// "Pangolin 1.24.0 (Enterprise)".
func (i Info) String() string {
	if i.Version == "" {
		return ""
	}
	s := "Pangolin " + i.Version
	switch strings.ToLower(i.Build) {
	case "enterprise":
		s += " (Enterprise)"
	case "oss":
		s += " (Community)"
	case "saas":
		s += " (Cloud)"
	}
	return s
}

// ExitNodesVersion is the first Pangolin release with exit nodes.
const ExitNodesVersion = "1.24.0"

// SupportsExitNodes reports whether a server version has exit nodes.
// Unknown or unparsable versions are assumed to.
func SupportsExitNodes(version string) bool {
	have, ok := parseVersion(version)
	if !ok {
		return true
	}
	want, _ := parseVersion(ExitNodesVersion)
	for i := range have {
		if have[i] != want[i] {
			return have[i] > want[i]
		}
	}
	return true
}

func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V")
	// Drop a pre-release tag ("-rc.1") and build metadata ("+abc").
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if v == "" || len(parts) > 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return [3]int{}, false
		}
		out[i] = n
	}
	return out, true
}

// Client is bound to one account on one server.
type Client struct {
	Host       string
	Token      string
	CookieName string
	HTTP       *http.Client
}

func (c *Client) baseURL() string {
	host := strings.TrimRight(strings.TrimSpace(c.Host), "/")
	// URL schemes are not case sensitive.
	if lower := strings.ToLower(host); !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		host = "https://" + host
	}
	return host + "/api/v1"
}

// checkRedirect stops redirect loops and refuses to follow a redirect from
// https to plain http, which would send the session cookie in the clear.
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	for _, prev := range via {
		if prev.URL.Scheme == "https" && req.URL.Scheme != "https" {
			return errors.New("refusing to follow a redirect from https to http")
		}
	}
	return nil
}

type envelope struct {
	Data    json.RawMessage `json:"data"`
	Success bool            `json:"success"`
	Error   json.RawMessage `json:"error"`
	Message string          `json:"message"`
}

func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	u := c.baseURL() + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-CSRF-Token", csrfToken)
	name := c.CookieName
	if name == "" {
		name = DefaultCookieName
	}
	req.AddCookie(&http.Cookie{Name: name, Value: c.Token})

	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second, CheckRedirect: checkRedirect}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	var env envelope
	parseErr := json.Unmarshal(body, &env)
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return ErrUnauthorized
	case resp.StatusCode == http.StatusForbidden:
		// Pangolin also answers 403 when the user may not see something.
		// That is not an expired session, so say what the server said.
		if parseErr == nil && env.Message != "" {
			return fmt.Errorf("GET %s: %s", path, env.Message)
		}
		return ErrUnauthorized
	}
	if parseErr != nil {
		return fmt.Errorf("GET %s: HTTP %d: unexpected response", path, resp.StatusCode)
	}
	if !env.Success || resp.StatusCode >= 400 {
		msg := env.Message
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return fmt.Errorf("GET %s: %s", path, msg)
	}
	if out == nil || len(env.Data) == 0 {
		return nil
	}
	return json.Unmarshal(env.Data, out)
}

// Info fetches the server's version and build.
func (c *Client) Info(ctx context.Context) (Info, error) {
	var data struct {
		Version string `json:"version"`
		Build   string `json:"build"`
	}
	if err := c.get(ctx, "/server-info", nil, &data); err != nil {
		return Info{}, err
	}
	return Info{Version: data.Version, Build: data.Build}, nil
}

// Orgs lists the organizations userID belongs to.
func (c *Client) Orgs(ctx context.Context, userID string) ([]Org, error) {
	var data struct {
		Orgs []struct {
			OrgID string `json:"orgId"`
			Name  string `json:"name"`
		} `json:"orgs"`
	}
	if err := c.get(ctx, "/user/"+url.PathEscape(userID)+"/orgs", nil, &data); err != nil {
		return nil, err
	}
	orgs := make([]Org, 0, len(data.Orgs))
	for _, o := range data.Orgs {
		if o.OrgID != "" {
			orgs = append(orgs, Org{ID: o.OrgID, Name: o.Name})
		}
	}
	return orgs, nil
}

// ExitNodes lists the usable exit nodes in orgID. Servers older than
// Pangolin 1.24 have none.
func (c *Client) ExitNodes(ctx context.Context, orgID string) ([]ExitNode, error) {
	type resource struct {
		SiteResourceID int      `json:"siteResourceId"`
		NiceID         string   `json:"niceId"`
		Name           string   `json:"name"`
		Mode           string   `json:"mode"`
		Enabled        bool     `json:"enabled"`
		SiteIDs        []int    `json:"siteIds"`
		SiteNames      []string `json:"siteNames"`
		SiteOnlines    []bool   `json:"siteOnlines"`
	}
	var nodes []ExitNode
	seen := map[int]bool{}
	// maxPages bounds the loop if a server ignores the page parameter or
	// keeps returning full pages.
	const maxPages = 100
	for page := 1; page <= maxPages; page++ {
		var data struct {
			SiteResources []resource `json:"siteResources"`
		}
		q := url.Values{
			"mode":     {"gateway"},
			"page":     {strconv.Itoa(page)},
			"pageSize": {strconv.Itoa(pageSize)},
		}
		if err := c.get(ctx, "/org/"+url.PathEscape(orgID)+"/site-resources", q, &data); err != nil {
			return nil, err
		}
		// Older servers ignore the mode filter and return every resource.
		fresh := 0
		for _, r := range data.SiteResources {
			// The list can shift between requests, and a server that ignores
			// the page number sends the same items again.
			if seen[r.SiteResourceID] {
				continue
			}
			seen[r.SiteResourceID] = true
			fresh++
			if r.Mode != "gateway" || !r.Enabled || len(r.SiteIDs) == 0 || r.NiceID == "" {
				continue
			}
			nodes = append(nodes, ExitNode{
				ResourceID: r.SiteResourceID,
				NiceID:     r.NiceID,
				Name:       r.Name,
				SiteIDs:    r.SiteIDs,
				SiteNames:  r.SiteNames,
				SiteOnline: r.SiteOnlines,
			})
		}
		if len(data.SiteResources) < pageSize || fresh == 0 {
			return nodes, nil
		}
	}
	return nodes, nil
}
