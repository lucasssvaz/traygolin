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
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// raw serves a fixed response.
func raw(t *testing.T, code int, contentType, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.WriteHeader(code)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return &Client{Host: srv.URL, Token: "tok"}
}

func TestBaseURLForms(t *testing.T) {
	for host, want := range map[string]string{
		"pangolin.example.com":           "https://pangolin.example.com/api/v1",
		"  pangolin.example.com  ":       "https://pangolin.example.com/api/v1",
		"pangolin.example.com:8443":      "https://pangolin.example.com:8443/api/v1",
		"HTTPS://Pangolin.Example.com":   "HTTPS://Pangolin.Example.com/api/v1",
		"Http://10.0.0.1:3000/":          "Http://10.0.0.1:3000/api/v1",
		"https://pangolin.example.com/":  "https://pangolin.example.com/api/v1",
		"https://[::1]:3000":             "https://[::1]:3000/api/v1",
		"[::1]:3000":                     "https://[::1]:3000/api/v1",
		"https://example.com/pangolin":   "https://example.com/pangolin/api/v1",
		"https://example.com/pangolin//": "https://example.com/pangolin/api/v1",
	} {
		if got := (&Client{Host: host}).baseURL(); got != want {
			t.Errorf("baseURL(%q) = %q, want %q", host, got, want)
		}
	}
	for _, host := range []string{"HTTPS://a.example", "Http://a.example", "hTtPs://a.example"} {
		if got := (&Client{Host: host}).baseURL(); strings.Count(strings.ToLower(got), "://") != 1 {
			t.Errorf("baseURL(%q) = %q has a doubled scheme", host, got)
		}
	}
}

func TestMixedCaseSchemeWorksEndToEnd(t *testing.T) {
	c := fakeServer(t, func(w http.ResponseWriter, r *http.Request) any {
		return map[string]any{"version": "1.30.0", "build": "oss"}
	})
	c.Host = "HTTP" + strings.TrimPrefix(c.Host, "http")
	if _, err := c.Info(t.Context()); err != nil {
		t.Fatalf("an upper-case scheme must not break the request: %v", err)
	}
}

func TestParseVersionForms(t *testing.T) {
	for in, want := range map[string]struct {
		v  [3]int
		ok bool
	}{
		"1.24.0":                   {[3]int{1, 24, 0}, true},
		"v1.24.0":                  {[3]int{1, 24, 0}, true},
		"V1.24.0":                  {[3]int{1, 24, 0}, true},
		" 1.24.0\n":                {[3]int{1, 24, 0}, true},
		"1.24":                     {[3]int{1, 24, 0}, true},
		"2":                        {[3]int{2, 0, 0}, true},
		"1.24.0-rc.1":              {[3]int{1, 24, 0}, true},
		"1.24.0+build.5":           {[3]int{1, 24, 0}, true},
		"1.23.9+meta":              {[3]int{1, 23, 9}, true},
		"1.24.0-rc.1+abc":          {[3]int{1, 24, 0}, true},
		"10.0.100":                 {[3]int{10, 0, 100}, true},
		"":                         {[3]int{}, false},
		"v":                        {[3]int{}, false},
		"dev":                      {[3]int{}, false},
		"1.2.3.4":                  {[3]int{}, false},
		"1..3":                     {[3]int{}, false},
		"1.":                       {[3]int{}, false},
		"1.2.":                     {[3]int{}, false},
		"1.-2.3":                   {[3]int{}, false},
		"a.b.c":                    {[3]int{}, false},
		"99999999999999999999.0.0": {[3]int{}, false},
	} {
		got, ok := parseVersion(in)
		// A failed parse must not hand back the parts it read before failing.
		if ok != want.ok || got != want.v {
			t.Errorf("parseVersion(%q) = %v %v, want %v %v", in, got, ok, want.v, want.ok)
		}
	}
}

func TestSupportsExitNodesBoundaries(t *testing.T) {
	for v, want := range map[string]bool{
		"1.23.99+meta":   false,
		"1.23.0+x":       false,
		"1.24.0+x":       true,
		"V1.23.0":        false,
		"V1.24.0":        true,
		"0.0.0":          false,
		"1.9.0":          false,
		"1.100.0":        true,
		"1.24.0-alpha":   true,
		"1.23.9-rc.1":    false,
		"  1.24.0  ":     true,
		"not a version":  true,
		"1.24.0.5":       true,
		"9999999999.0.0": true,
	} {
		if got := SupportsExitNodes(v); got != want {
			t.Errorf("SupportsExitNodes(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestResponseHandling(t *testing.T) {
	for name, tc := range map[string]struct {
		code        int
		ctype, body string
		wantErr     string
	}{
		"ok":                   {200, "application/json", `{"success":true,"data":{"version":"1.24.0"}}`, ""},
		"ok without data":      {200, "application/json", `{"success":true}`, ""},
		"data null":            {200, "application/json", `{"success":true,"data":null}`, ""},
		"success false":        {200, "application/json", `{"success":false,"message":"Nope"}`, "Nope"},
		"success false silent": {200, "application/json", `{"success":false}`, "OK"},
		"error status":         {400, "application/json", `{"success":true,"data":{}}`, "Bad Request"},
		"error with message":   {422, "application/json", `{"success":false,"message":"Invalid input"}`, "Invalid input"},
		"html 502":             {502, "text/html", `<html>Bad gateway</html>`, "unexpected response"},
		"empty 200":            {200, "", ``, "unexpected response"},
		"empty 204":            {204, "", ``, "unexpected response"},
		"truncated json":       {200, "application/json", `{"success":true,"data":{"vers`, "unexpected response"},
		"wrong data type":      {200, "application/json", `{"success":true,"data":"text"}`, "cannot unmarshal"},
		"401":                  {401, "application/json", `{"success":false,"message":"Unauthorized"}`, ErrUnauthorized.Error()},
		"401 html":             {401, "text/html", `<html>`, ErrUnauthorized.Error()},
		"403 bare":             {403, "", ``, ErrUnauthorized.Error()},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := raw(t, tc.code, tc.ctype, tc.body).Info(t.Context())
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Fatalf("err = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestForbiddenForAnotherReasonIsNotAnExpiredSession(t *testing.T) {
	// Pangolin answers 403 when the user may not see an organization. That
	// is not a reason to ask them to log in again.
	c := raw(t, 403, "application/json", `{"success":false,"error":true,"message":"User does not have access to this organization"}`)
	_, err := c.ExitNodes(t.Context(), "someone-elses")
	if err == nil || errors.Is(err, ErrUnauthorized) || !strings.Contains(err.Error(), "does not have access") {
		t.Fatalf("err = %v", err)
	}
}

func TestHugeResponseDoesNotExhaustMemory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"success":true,"data":{"version":"`)
		chunk := strings.Repeat("a", 1<<20)
		for range 64 {
			fmt.Fprint(w, chunk)
		}
		fmt.Fprint(w, `"}}`)
	}))
	t.Cleanup(srv.Close)
	c := &Client{Host: srv.URL, Token: "tok"}
	if _, err := c.Info(t.Context()); err == nil {
		t.Fatal("a 64 MB reply should be refused")
	}
}

func TestRequestShape(t *testing.T) {
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		fmt.Fprint(w, `{"success":true,"data":{"orgs":[]}}`)
	}))
	t.Cleanup(srv.Close)
	c := &Client{Host: srv.URL, Token: "tok", CookieName: "custom_cookie"}
	if _, err := c.Orgs(t.Context(), "user/with space?and#chars"); err != nil {
		t.Fatal(err)
	}
	if got.Method != "GET" || got.Header.Get("X-CSRF-Token") != csrfToken || got.Header.Get("Accept") != "application/json" {
		t.Errorf("headers %v", got.Header)
	}
	if got.URL.EscapedPath() != "/api/v1/user/user%2Fwith%20space%3Fand%23chars/orgs" {
		t.Errorf("path %q (raw %q)", got.URL.Path, got.URL.RawPath)
	}
	if got.URL.RawQuery != "" {
		t.Errorf("a user id leaked into the query: %q", got.URL.RawQuery)
	}
	if len(got.Cookies()) != 1 || got.Cookies()[0].Name != "custom_cookie" {
		t.Errorf("cookies %v", got.Cookies())
	}
	if got.Header.Get("Authorization") != "" {
		t.Error("no bearer token is used")
	}
}

func TestOrgsEdgeCases(t *testing.T) {
	for name, tc := range map[string]struct {
		data string
		want []Org
	}{
		"none":        {`{"orgs":[]}`, []Org{}},
		"null":        {`{"orgs":null}`, []Org{}},
		"missing":     {`{}`, []Org{}},
		"unicode":     {`{"orgs":[{"orgId":"ö","name":"日本語 🏠"}]}`, []Org{{"ö", "日本語 🏠"}}},
		"empty ids":   {`{"orgs":[{"orgId":""},{"name":"x"},{"orgId":"ok"}]}`, []Org{{"ok", ""}}},
		"keeps order": {`{"orgs":[{"orgId":"b"},{"orgId":"a"},{"orgId":"c"}]}`, []Org{{"b", ""}, {"a", ""}, {"c", ""}}},
		"duplicates":  {`{"orgs":[{"orgId":"a"},{"orgId":"a"}]}`, []Org{{"a", ""}, {"a", ""}}},
	} {
		c := raw(t, 200, "application/json", `{"success":true,"data":`+tc.data+`}`)
		got, err := c.Orgs(t.Context(), "u")
		if err != nil || len(got) != len(tc.want) {
			t.Errorf("%s: %v %v", name, got, err)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%s: %v, want %v", name, got, tc.want)
			}
		}
	}
}

func TestOrgLabelAndExitNodeHelpers(t *testing.T) {
	if (Org{ID: "i"}).Label() != "i" || (Org{ID: "i", Name: "n"}).Label() != "n" || (Org{}).Label() != "" {
		t.Error("org label")
	}
	if (ExitNode{NiceID: "n"}).Label() != "n" || (ExitNode{NiceID: "n", Name: "N"}).Label() != "N" {
		t.Error("exit node label")
	}
	for name, tc := range map[string]struct {
		on   []bool
		want bool
	}{
		"unknown":     {nil, true},
		"all down":    {[]bool{false, false}, false},
		"one up":      {[]bool{false, true}, true},
		"single up":   {[]bool{true}, true},
		"single down": {[]bool{false}, false},
	} {
		if got := (ExitNode{SiteOnline: tc.on}).Online(); got != tc.want {
			t.Errorf("%s: %v", name, got)
		}
	}
}

func TestInfoStrings(t *testing.T) {
	for in, want := range map[Info]string{
		{}:                                      "",
		{Build: "oss"}:                          "",
		{Version: "1.0.0"}:                      "Pangolin 1.0.0",
		{Version: "1.0.0", Build: "weird"}:      "Pangolin 1.0.0",
		{Version: "1.0.0", Build: "SaaS"}:       "Pangolin 1.0.0 (Cloud)",
		{Version: "1.0.0", Build: "OSS"}:        "Pangolin 1.0.0 (Community)",
		{Version: "1.0.0", Build: "Enterprise"}: "Pangolin 1.0.0 (Enterprise)",
	} {
		if got := in.String(); got != want {
			t.Errorf("%+v: %q, want %q", in, got, want)
		}
	}
}

func resources(from, n int, mode string) []map[string]any {
	out := make([]map[string]any, n)
	for i := range out {
		id := from + i
		out[i] = map[string]any{
			"siteResourceId": id, "niceId": fmt.Sprintf("gw-%d", id), "mode": mode, "enabled": true,
			"siteIds": []int{id},
		}
	}
	return out
}

func TestExitNodesPaginationBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name     string
		total    int
		requests int
	}{
		{"empty", 0, 1},
		{"one", 1, 1},
		{"one short of a page", pageSize - 1, 1},
		{"exactly a page", pageSize, 2},
		{"a page and one", pageSize + 1, 2},
		{"two pages", 2 * pageSize, 3},
		{"five pages and a bit", 5*pageSize + 7, 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int64
			c := fakeServer(t, func(w http.ResponseWriter, r *http.Request) any {
				requests.Add(1)
				page := 1
				fmt.Sscan(r.URL.Query().Get("page"), &page)
				start := (page - 1) * pageSize
				n := min(max(tc.total-start, 0), pageSize)
				return map[string]any{"siteResources": resources(start+1, n, "gateway")}
			})
			nodes, err := c.ExitNodes(t.Context(), "o")
			if err != nil || len(nodes) != tc.total {
				t.Fatalf("%d nodes, %v", len(nodes), err)
			}
			if int(requests.Load()) != tc.requests {
				t.Errorf("%d requests, want %d", requests.Load(), tc.requests)
			}
			seen := map[int]bool{}
			for _, n := range nodes {
				if seen[n.ResourceID] {
					t.Fatalf("node %d listed twice", n.ResourceID)
				}
				seen[n.ResourceID] = true
			}
		})
	}
}

// TestExitNodesStopsWhenThePagerLoops covers a server or proxy that ignores
// the page parameter. Without a guard the tray would request the same full
// page forever, once per refresh.
func TestExitNodesStopsWhenThePagerLoops(t *testing.T) {
	var requests atomic.Int64
	c := fakeServer(t, func(w http.ResponseWriter, r *http.Request) any {
		if requests.Add(1) > 500 {
			return map[string]any{"siteResources": []any{}}
		}
		return map[string]any{"siteResources": resources(1, pageSize, "gateway")}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	nodes, err := c.ExitNodes(ctx, "o")
	if n := requests.Load(); n > 120 {
		t.Fatalf("made %d requests for one list", n)
	}
	if err == nil && len(nodes) != pageSize {
		t.Errorf("repeated pages must not repeat nodes: got %d", len(nodes))
	}
}

func TestExitNodesDeduplicatesAcrossPages(t *testing.T) {
	// The list shifts while it is being paged through, so the last item of
	// page one shows up again at the top of page two.
	c := fakeServer(t, func(w http.ResponseWriter, r *http.Request) any {
		if r.URL.Query().Get("page") == "1" {
			return map[string]any{"siteResources": resources(1, pageSize, "gateway")}
		}
		return map[string]any{"siteResources": resources(pageSize, 3, "gateway")}
	})
	nodes, err := c.ExitNodes(t.Context(), "o")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int]int{}
	for _, n := range nodes {
		seen[n.ResourceID]++
	}
	for id, n := range seen {
		if n > 1 {
			t.Errorf("resource %d listed %d times", id, n)
		}
	}
	if len(nodes) != pageSize+2 {
		t.Errorf("%d nodes", len(nodes))
	}
}

func TestExitNodesErrorOnLaterPageDiscardsPartialList(t *testing.T) {
	c := fakeServer(t, func(w http.ResponseWriter, r *http.Request) any {
		if r.URL.Query().Get("page") == "1" {
			return map[string]any{"siteResources": resources(1, pageSize, "gateway")}
		}
		w.WriteHeader(500)
		return nil
	})
	nodes, err := c.ExitNodes(t.Context(), "o")
	if err == nil || nodes != nil {
		t.Fatalf("a partial list must not be shown as complete: %d nodes, %v", len(nodes), err)
	}
}

func TestExitNodesFilters(t *testing.T) {
	c := fakeServer(t, func(w http.ResponseWriter, r *http.Request) any {
		return map[string]any{"siteResources": []map[string]any{
			{"siteResourceId": 1, "niceId": "ok", "mode": "gateway", "enabled": true, "siteIds": []int{1}},
			{"siteResourceId": 2, "niceId": "http", "mode": "http", "enabled": true, "siteIds": []int{1}},
			{"siteResourceId": 3, "niceId": "off", "mode": "gateway", "enabled": false, "siteIds": []int{1}},
			{"siteResourceId": 4, "niceId": "nosites", "mode": "gateway", "enabled": true, "siteIds": []int{}},
			{"siteResourceId": 5, "niceId": "nullsites", "mode": "gateway", "enabled": true},
			{"siteResourceId": 6, "niceId": "", "mode": "gateway", "enabled": true, "siteIds": []int{1}},
			{"siteResourceId": 8, "niceId": "shorter-names", "mode": "gateway", "enabled": true, "siteIds": []int{1, 2, 3}, "siteNames": []string{"a"}, "siteOnlines": []bool{true}},
		}}
	})
	nodes, err := c.ExitNodes(t.Context(), "o")
	if err != nil {
		t.Fatal(err)
	}
	var ids []int
	for _, n := range nodes {
		ids = append(ids, n.ResourceID)
	}
	if fmt.Sprint(ids) != "[1 8]" {
		t.Fatalf("ids %v", ids)
	}
	// Per-site slices may be shorter than the site list.
	_ = nodes[1].Online()
	_ = nodes[1].Label()
}

func TestExitNodesRequestsTheRightThing(t *testing.T) {
	var q url.Values
	var path string
	c := fakeServer(t, func(w http.ResponseWriter, r *http.Request) any {
		q, path = r.URL.Query(), r.URL.EscapedPath()
		return map[string]any{"siteResources": []any{}}
	})
	if _, err := c.ExitNodes(t.Context(), "org/with space"); err != nil {
		t.Fatal(err)
	}
	if path != "/api/v1/org/org%2Fwith%20space/site-resources" || q.Get("mode") != "gateway" || q.Get("page") != "1" || q.Get("pageSize") != "100" {
		t.Errorf("%s %v", path, q)
	}
}

func TestCanceledContext(t *testing.T) {
	c := fakeServer(t, func(w http.ResponseWriter, r *http.Request) any { return map[string]any{} })
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.Info(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("%v", err)
	}
}

func TestSlowServerHonorsTheClientTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(release); srv.Close() })
	c := &Client{Host: srv.URL, Token: "tok", HTTP: &http.Client{Timeout: 200 * time.Millisecond}}
	start := time.Now()
	if _, err := c.Info(t.Context()); err == nil {
		t.Fatal("expected a timeout")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("did not time out")
	}
}

func TestRefusesToDowngradeToPlainHTTP(t *testing.T) {
	// Following a redirect from https to http would send the session cookie
	// over an unencrypted connection.
	via := []*http.Request{{URL: &url.URL{Scheme: "https", Host: "pangolin.example"}}}
	to := &http.Request{URL: &url.URL{Scheme: "http", Host: "pangolin.example"}}
	if err := checkRedirect(to, via); err == nil {
		t.Fatal("https to http redirect allowed")
	}
	same := &http.Request{URL: &url.URL{Scheme: "https", Host: "pangolin.example", Path: "/other"}}
	if err := checkRedirect(same, via); err != nil {
		t.Errorf("https to https redirect refused: %v", err)
	}
	local := []*http.Request{{URL: &url.URL{Scheme: "http", Host: "10.0.0.1:3000"}}}
	if err := checkRedirect(&http.Request{URL: &url.URL{Scheme: "http", Host: "10.0.0.1:3000", Path: "/x"}}, local); err != nil {
		t.Errorf("http to http redirect refused: %v", err)
	}
	var many []*http.Request
	for range 12 {
		many = append(many, via[0])
	}
	if err := checkRedirect(same, many); err == nil {
		t.Error("redirect loops must end")
	}
}

func TestCookieIsNotSentToAnotherHost(t *testing.T) {
	var leaked atomic.Bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := r.Cookie(DefaultCookieName); err == nil {
			leaked.Store(true)
		}
		fmt.Fprint(w, `{"success":true,"data":{}}`)
	}))
	t.Cleanup(other.Close)
	// "localhost" and "127.0.0.1" are different hosts to the HTTP client.
	target := strings.Replace(other.URL, "127.0.0.1", "localhost", 1)
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target+r.URL.Path, http.StatusFound)
	}))
	t.Cleanup(first.Close)
	c := &Client{Host: first.URL, Token: "tok"}
	_, _ = c.Info(t.Context())
	if leaked.Load() {
		t.Fatal("the session cookie followed a redirect to another host")
	}
}

func TestConcurrentUse(t *testing.T) {
	var served atomic.Int64
	c := fakeServer(t, func(w http.ResponseWriter, r *http.Request) any {
		served.Add(1)
		switch {
		case strings.HasSuffix(r.URL.Path, "/orgs"):
			return map[string]any{"orgs": []map[string]any{{"orgId": "a", "name": "A"}}}
		case strings.HasSuffix(r.URL.Path, "/server-info"):
			return map[string]any{"version": "1.24.0", "build": "oss"}
		}
		return map[string]any{"siteResources": resources(1, 3, "gateway")}
	})
	var wg sync.WaitGroup
	var bad atomic.Int64
	for i := range 48 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				switch i % 3 {
				case 0:
					if o, err := c.Orgs(t.Context(), "u"); err != nil || len(o) != 1 {
						bad.Add(1)
					}
				case 1:
					if in, err := c.Info(t.Context()); err != nil || in.Version != "1.24.0" {
						bad.Add(1)
					}
				case 2:
					if n, err := c.ExitNodes(t.Context(), "o"); err != nil || len(n) != 3 {
						bad.Add(1)
					}
				}
			}
		}()
	}
	wg.Wait()
	if bad.Load() != 0 || served.Load() != 48*20 {
		t.Fatalf("%d bad, %d served", bad.Load(), served.Load())
	}
}

func FuzzParseVersion(f *testing.F) {
	for _, s := range []string{"1.24.0", "v1", "", "1.2.3-rc.1+b", "..", "-", "+", "9" + strings.Repeat("9", 30)} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		v, ok := parseVersion(s)
		_ = SupportsExitNodes(s)
		if !ok && v != ([3]int{}) {
			t.Fatalf("%q: not ok but %v", s, v)
		}
		for _, n := range v {
			if n < 0 {
				t.Fatalf("%q: negative component %v", s, v)
			}
		}
	})
}

func FuzzEnvelope(f *testing.F) {
	f.Add(200, `{"success":true,"data":{"orgs":[{"orgId":"a"}]}}`)
	f.Add(500, `<html>`)
	f.Add(403, `{"success":false,"message":"x"}`)
	f.Fuzz(func(t *testing.T, code int, body string) {
		if code < 200 || code > 599 || code == 204 || code == 304 || (code >= 100 && code < 200) {
			return
		}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
			fmt.Fprint(w, body)
		}))
		defer srv.Close()
		c := &Client{Host: srv.URL, Token: "t"}
		orgs, err := c.Orgs(t.Context(), "u")
		if err == nil {
			for _, o := range orgs {
				if o.ID == "" {
					t.Fatal("org without an ID")
				}
			}
		}
	})
}
