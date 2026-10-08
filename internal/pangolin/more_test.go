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
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestServerClientEmptyIDNeverMatches(t *testing.T) {
	// A record without a userId has an empty UserID field. Asking for ""
	// (nobody logged in) must not hand out that record's session token.
	dir := withConfigHome(t)
	doc := `{"accounts":{"k":{"host":"https://h.example","sessionToken":"secret-token"}}}`
	if err := os.WriteFile(filepath.Join(dir, "accounts.json"), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	if c, err := ServerClient(""); err == nil {
		t.Fatalf("got a client for an empty user ID (host %q)", c.Host)
	}
	if c, err := ServerClient("k"); err != nil || c.Token != "secret-token" {
		t.Fatalf("lookup by key: %v", err)
	}
}

func TestServerClientPrefersTheMapKey(t *testing.T) {
	// "a" is both a map key and another record's userId. The key is the
	// account's identity, so it wins, every time.
	dir := withConfigHome(t)
	doc := `{"accounts":{
	  "a":{"userId":"a","host":"https://key.example","sessionToken":"by-key"},
	  "b":{"userId":"a","host":"https://field.example","sessionToken":"by-field"}}}`
	if err := os.WriteFile(filepath.Join(dir, "accounts.json"), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	for range 50 {
		c, err := ServerClient("a")
		if err != nil {
			t.Fatal(err)
		}
		if c.Token != "by-key" {
			t.Fatalf("matched the userId field (%q) instead of the key", c.Host)
		}
	}
}

func TestServerClientFieldMatchIsDeterministic(t *testing.T) {
	// Two records whose keys differ from their shared userId. Map order is
	// random, so without sorting the chosen server would flip between calls.
	dir := withConfigHome(t)
	var b strings.Builder
	b.WriteString(`{"accounts":{`)
	for i := range 8 {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `"k%d":{"userId":"shared","host":"https://h%d.example","sessionToken":"t%d"}`, i, i, i)
	}
	b.WriteString(`}}`)
	if err := os.WriteFile(filepath.Join(dir, "accounts.json"), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := ServerClient("shared")
	if err != nil {
		t.Fatal(err)
	}
	for range 100 {
		c, err := ServerClient("shared")
		if err != nil {
			t.Fatal(err)
		}
		if c.Host != first.Host {
			t.Fatalf("got %s then %s", first.Host, c.Host)
		}
	}
}

func TestLoginEventReportsEveryNewURL(t *testing.T) {
	bin := writeFakeCLI(t, `echo "Open https://a.example/one or https://a.example/two."
echo "Again https://a.example/two and https://a.example/three"
echo "No links here"`)
	var events []LoginEvent
	_, err := (&Client{Binary: bin}).Login(t.Context(), "", func(ev LoginEvent) {
		events = append(events, ev)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("%d events", len(events))
	}
	want := [][]string{
		{"https://a.example/one", "https://a.example/two"},
		{"https://a.example/three"},
		nil,
	}
	for i, ev := range events {
		if !slices.Equal(ev.URLs, want[i]) {
			t.Errorf("line %d: URLs %q, want %q", i, ev.URLs, want[i])
		}
		first := ""
		if len(want[i]) > 0 {
			first = want[i][0]
		}
		if ev.URL != first {
			t.Errorf("line %d: URL %q, want %q", i, ev.URL, first)
		}
	}
}

func TestStreamKeepsOnlyTheEndOfLongOutput(t *testing.T) {
	// 20,000 lines of 100 bytes is 2 MB. The result keeps a bounded tail of
	// whole lines, ending with the last one.
	bin := writeFakeCLI(t, `i=0; while [ $i -lt 20000 ]; do printf '%099d\n' $i; i=$((i+1)); done; echo LAST; exit 3`)
	var n int
	out, err := (&Client{Binary: bin}).stream(t.Context(), 30*time.Second, func(string) { n++ }, "x")
	if err == nil {
		t.Fatal("the CLI failed")
	}
	if n != 20001 {
		t.Errorf("onLine saw %d lines, want every one", n)
	}
	if len(out) > 2*maxKept {
		t.Errorf("kept %d bytes", len(out))
	}
	if !strings.HasSuffix(out, "LAST") {
		t.Errorf("lost the last line: ...%q", out[max(0, len(out)-40):])
	}
	for i, line := range strings.Split(out, "\n") {
		if line != "LAST" && len(line) != 99 {
			t.Fatalf("line %d is cut: %q", i, line)
		}
	}
	if !strings.Contains(err.Error(), "LAST") {
		t.Errorf("error does not end with the last output: %v", err)
	}
}

func TestLineSinkConcurrentWriters(t *testing.T) {
	// stdout and stderr are written from separate goroutines by exec.
	var mu sync.Mutex
	seen := map[string]int{}
	sink := &lineSink{onLine: func(l string) {
		mu.Lock()
		seen[l]++
		mu.Unlock()
	}}
	var wg sync.WaitGroup
	for w := range 4 {
		lw := &lineWriter{sink: sink}
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 2000 {
				// Split each line over two writes to exercise pending data.
				line := fmt.Sprintf("w%d-%d", w, i)
				lw.Write([]byte(line[:2]))
				lw.Write([]byte(line[2:] + "\n"))
			}
			lw.flush()
		}()
	}
	wg.Wait()
	if len(seen) != 8000 {
		t.Fatalf("%d distinct lines, want 8000", len(seen))
	}
	for l, c := range seen {
		if c != 1 {
			t.Fatalf("%q seen %d times", l, c)
		}
	}
}

func TestLineWriterSplitsAnywhere(t *testing.T) {
	// The same output cut into chunks of every size gives the same lines.
	text := "one\r\ntwo\n\nthree\nlast-without-newline"
	want := []string{"one", "two", "", "three", "last-without-newline"}
	for size := 1; size <= len(text); size++ {
		var got []string
		lw := &lineWriter{sink: &lineSink{onLine: func(l string) { got = append(got, l) }}}
		for i := 0; i < len(text); i += size {
			lw.Write([]byte(text[i:min(i+size, len(text))]))
		}
		lw.flush()
		if !slices.Equal(got, want) {
			t.Fatalf("chunk size %d: %q", size, got)
		}
	}
}

func TestLatestVersionIgnoresALingeringChild(t *testing.T) {
	bin := writeFakeCLI(t, `echo "pangolin version 1.0.0"; echo "A new version is available: v1.2.0"; sleep 8 &`)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	start := time.Now()
	v, err := (&Client{Binary: bin}).LatestVersion(ctx)
	if err != nil {
		t.Fatalf("a child holding the output open is not a failure: %v", err)
	}
	if v != "1.2.0" {
		t.Errorf("version %q", v)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("waited %v for the child", d)
	}
}
