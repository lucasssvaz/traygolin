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

package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func fakeServer(t *testing.T, h func(w http.ResponseWriter, r *http.Request) any) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(DefaultCookieName); err != nil || c.Value != "tok" {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": true, "message": "Unauthorized"})
			return
		}
		if r.Header.Get("X-CSRF-Token") != csrfToken {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		data := h(w, r)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "error": false, "data": data})
	}))
	t.Cleanup(srv.Close)
	return &Client{Host: srv.URL, Token: "tok"}
}

func TestOrgs(t *testing.T) {
	c := fakeServer(t, func(w http.ResponseWriter, r *http.Request) any {
		if r.URL.Path != "/api/v1/user/u1/orgs" {
			t.Errorf("path = %s", r.URL.Path)
		}
		return map[string]any{"orgs": []map[string]any{
			{"orgId": "home", "name": "Home"},
			{"orgId": "", "name": "broken"},
			{"orgId": "work"},
		}}
	})
	orgs, err := c.Orgs(t.Context(), "u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(orgs) != 2 || orgs[0] != (Org{ID: "home", Name: "Home"}) || orgs[1].Label() != "work" {
		t.Fatalf("orgs = %+v", orgs)
	}
}

func TestExitNodesFiltersAndPaginates(t *testing.T) {
	var pages []string
	c := fakeServer(t, func(w http.ResponseWriter, r *http.Request) any {
		if r.URL.Path != "/api/v1/org/home/site-resources" || r.URL.Query().Get("mode") != "gateway" {
			t.Errorf("request = %s", r.URL)
		}
		page := r.URL.Query().Get("page")
		pages = append(pages, page)
		var res []map[string]any
		if page == "1" {
			for i := range pageSize {
				res = append(res, map[string]any{"siteResourceId": i, "niceId": "http-" + strconv.Itoa(i), "mode": "http", "enabled": true, "siteIds": []int{1}})
			}
			res[0] = map[string]any{"siteResourceId": 7, "niceId": "us-east", "name": "US East", "mode": "gateway", "enabled": true, "siteIds": []int{3, 4}, "siteNames": []string{"a", "b"}, "siteOnlines": []bool{false, true}}
			res[1] = map[string]any{"siteResourceId": 8, "niceId": "off", "mode": "gateway", "enabled": false, "siteIds": []int{5}}
		} else {
			res = append(res,
				map[string]any{"siteResourceId": 9, "niceId": "empty", "mode": "gateway", "enabled": true},
				map[string]any{"siteResourceId": 10, "niceId": "eu", "mode": "gateway", "enabled": true, "siteIds": []int{6}, "siteOnlines": []bool{false}},
			)
		}
		return map[string]any{"siteResources": res}
	})
	nodes, err := c.ExitNodes(t.Context(), "home")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(pages) != "[1 2]" {
		t.Fatalf("pages = %v", pages)
	}
	if len(nodes) != 2 || nodes[0].NiceID != "us-east" || nodes[1].NiceID != "eu" {
		t.Fatalf("nodes = %+v", nodes)
	}
	if nodes[0].ResourceID != 7 || !nodes[0].Online() || nodes[1].Online() || nodes[1].Label() != "eu" {
		t.Fatalf("nodes = %+v", nodes)
	}
}

func TestUnauthorized(t *testing.T) {
	c := fakeServer(t, func(http.ResponseWriter, *http.Request) any { return nil })
	c.Token = "expired"
	if _, err := c.Orgs(t.Context(), "u1"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v", err)
	}
}

func TestAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": true, "message": "Organization not found"})
	}))
	t.Cleanup(srv.Close)
	c := &Client{Host: srv.URL, Token: "tok"}
	if _, err := c.ExitNodes(t.Context(), "nope"); err == nil || err.Error() != "GET /org/nope/site-resources: Organization not found" {
		t.Fatalf("err = %v", err)
	}
}

func TestInfo(t *testing.T) {
	c := fakeServer(t, func(w http.ResponseWriter, r *http.Request) any {
		if r.URL.Path != "/api/v1/server-info" {
			t.Errorf("path = %s", r.URL.Path)
		}
		return map[string]any{"version": "1.21.1", "build": "enterprise", "enterpriseLicenseValid": true}
	})
	info, err := c.Info(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if info != (Info{Version: "1.21.1", Build: "enterprise"}) || info.String() != "Pangolin 1.21.1 (Enterprise)" {
		t.Fatalf("%+v %q", info, info)
	}
	if (Info{Version: "1.24.0", Build: "oss"}).String() != "Pangolin 1.24.0 (Community)" || (Info{}).String() != "" {
		t.Fatal("info strings")
	}
}

func TestSupportsExitNodes(t *testing.T) {
	for v, want := range map[string]bool{
		"1.21.1": false, "1.23.9": false, "0.99.0": false,
		"1.24.0": true, "v1.24.0": true, "1.24.0-rc.1": true, "1.24": true, "1.25.3": true, "2.0.0": true,
		"": true, "dev": true, "1.x": true,
	} {
		if got := SupportsExitNodes(v); got != want {
			t.Errorf("SupportsExitNodes(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestBaseURL(t *testing.T) {
	for host, want := range map[string]string{
		"pangolin.example.com":          "https://pangolin.example.com/api/v1",
		"https://pangolin.example.com/": "https://pangolin.example.com/api/v1",
		"http://10.0.0.1:3000":          "http://10.0.0.1:3000/api/v1",
	} {
		if got := (&Client{Host: host}).baseURL(); got != want {
			t.Errorf("baseURL(%q) = %q, want %q", host, got, want)
		}
	}
}
