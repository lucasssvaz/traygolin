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
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func readAccounts(t *testing.T, path string) (active string, accounts map[string]map[string]json.RawMessage) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Active   string                                `json:"activeuserid"`
		Accounts map[string]map[string]json.RawMessage `json:"accounts"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("accounts.json is not valid JSON: %v\n%s", err, data)
	}
	return doc.Active, doc.Accounts
}

func TestClearSavedExitNodeEdgeCases(t *testing.T) {
	for name, tc := range map[string]struct {
		doc     string
		wantErr bool
		// untouched means the file must be byte for byte the same.
		untouched bool
		// cleared names the account that must lose its exit node.
		cleared string
		// kept names an account that must keep its exit node.
		kept string
	}{
		"no active user":        {doc: `{"activeuserid":"","accounts":{"a":{"exitNodeResourceId":4}}}`, untouched: true, kept: "a"},
		"missing active key":    {doc: `{"accounts":{"a":{"exitNodeResourceId":4}}}`, untouched: true, kept: "a"},
		"active has no account": {doc: `{"activeuserid":"zzz","accounts":{"a":{"exitNodeResourceId":4}}}`, untouched: true, kept: "a"},
		"nothing to clear":      {doc: `{"activeuserid":"a","accounts":{"a":{"userId":"a"}}}`, untouched: true},
		"accounts is null":      {doc: `{"activeuserid":"a","accounts":null}`, untouched: true},
		"no accounts key":       {doc: `{"activeuserid":"a"}`, untouched: true},
		"invalid json":          {doc: `{"activeuserid":`, wantErr: true, untouched: true},
		"not an object":         {doc: `[1,2,3]`, wantErr: true, untouched: true},
		"empty file":            {doc: ``, wantErr: true, untouched: true},
		"clears only active":    {doc: `{"activeuserid":"a","accounts":{"a":{"exitNodeResourceId":4},"b":{"exitNodeResourceId":5}}}`, cleared: "a", kept: "b"},
		"key differs":           {doc: `{"activeuserid":"u1","accounts":{"k1":{"userId":"u1","exitNodeResourceId":4}}}`, cleared: "k1"},
		"odd case":              {doc: `{"activeuserid":"a","accounts":{"a":{"ExitNodeResourceID":4}}}`, cleared: "a"},
		"null exit node":        {doc: `{"activeuserid":"a","accounts":{"a":{"exitNodeResourceId":null}}}`, cleared: "a"},
		"active not a string":   {doc: `{"activeuserid":7,"accounts":{"a":{"exitNodeResourceId":4}}}`, untouched: true},
	} {
		t.Run(name, func(t *testing.T) {
			dir := withConfigHome(t)
			path := filepath.Join(dir, "accounts.json")
			if err := os.WriteFile(path, []byte(tc.doc), 0o600); err != nil {
				t.Fatal(err)
			}
			err := ClearSavedExitNode()
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, want error %v", err, tc.wantErr)
			}
			after, _ := os.ReadFile(path)
			if tc.untouched && string(after) != tc.doc {
				t.Fatalf("file changed:\n%s", after)
			}
			if tc.cleared != "" || tc.kept != "" {
				_, accs := readAccounts(t, path)
				if tc.cleared != "" {
					for k := range accs[tc.cleared] {
						if strings.EqualFold(k, "exitNodeResourceId") {
							t.Errorf("%s still has %s", tc.cleared, k)
						}
					}
				}
				if tc.kept != "" {
					if _, ok := accs[tc.kept]["exitNodeResourceId"]; !ok {
						t.Errorf("%s lost its exit node", tc.kept)
					}
				}
			}
			// No temporary files are left behind, whatever happened.
			entries, _ := os.ReadDir(dir)
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), ".accounts-") {
					t.Errorf("leftover temp file %s", e.Name())
				}
			}
		})
	}
}

func TestClearSavedExitNodeKeepsEverythingElse(t *testing.T) {
	dir := withConfigHome(t)
	path := filepath.Join(dir, "accounts.json")
	const doc = `{
  "activeuserid": "a",
  "future": {"nested": [1, 2.50, "x", null, true], "big": 12345678901234567890},
  "accounts": {
    "a": {"userId": "a", "sessionToken": "TOK", "exitNodeResourceId": 4, "unicode": "j\u00f6rg", "n": 1.0},
    "b": {"userId": "b", "exitNodeResourceId": 9}
  }
}`
	if err := os.WriteFile(path, []byte(doc), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := ClearSavedExitNode(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	for _, want := range []string{`12345678901234567890`, `2.50`, `"TOK"`, `"n": 1.0`, `"exitNodeResourceId": 9`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("lost %q:\n%s", want, data)
		}
	}
	if strings.Contains(string(data), `"exitNodeResourceId": 4`) {
		t.Errorf("not cleared:\n%s", data)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o640 {
		t.Errorf("mode %v", info.Mode().Perm())
	}
}

func TestClearSavedExitNodeThroughASymlink(t *testing.T) {
	dir := withConfigHome(t)
	real := filepath.Join(t.TempDir(), "dotfiles-accounts.json")
	if err := os.WriteFile(real, []byte(`{"activeuserid":"a","accounts":{"a":{"exitNodeResourceId":4}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "accounts.json")
	if err := os.Symlink(real, link); err != nil {
		t.Skip(err)
	}
	if err := ClearSavedExitNode(); err != nil {
		t.Fatal(err)
	}
	// Dotfile managers link the config in. Replacing the link with a regular
	// file would silently detach it from the managed copy.
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("symlink was replaced by a regular file")
	}
	_, accs := readAccounts(t, real)
	if _, ok := accs["a"]["exitNodeResourceId"]; ok {
		t.Error("the file behind the link was not updated")
	}
}

func TestClearSavedExitNodeMissingFile(t *testing.T) {
	withConfigHome(t)
	if err := ClearSavedExitNode(); err != nil {
		t.Fatal(err)
	}
}

func TestClearSavedExitNodeReadOnlyDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := withConfigHome(t)
	path := filepath.Join(dir, "accounts.json")
	const doc = `{"activeuserid":"a","accounts":{"a":{"exitNodeResourceId":4}}}`
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	if err := ClearSavedExitNode(); err == nil {
		t.Error("expected a permission error")
	}
	if data, _ := os.ReadFile(path); string(data) != doc {
		t.Errorf("file damaged: %s", data)
	}
}

// TestAccountsFileIsNeverSeenHalfWritten hammers the file from several
// writers while readers poll it, as the tray does. A reader must always see
// either the old or the new document, never a torn one, and a logged-in
// user must never appear logged out.
func TestAccountsFileIsNeverSeenHalfWritten(t *testing.T) {
	dir := withConfigHome(t)
	path := filepath.Join(dir, "accounts.json")
	const doc = `{"activeuserid":"u1","accounts":{"u1":{"userId":"u1","host":"https://h.example","email":"a@b","sessionToken":"SECRET-TOKEN","exitNodeResourceId":4}}}`
	write := func() {
		tmp, err := os.CreateTemp(dir, ".seed-*")
		if err != nil {
			t.Error(err)
			return
		}
		tmp.WriteString(doc)
		tmp.Close()
		os.Chmod(tmp.Name(), 0o600)
		if err := os.Rename(tmp.Name(), path); err != nil {
			t.Error(err)
		}
	}
	write()

	var stop atomic.Bool
	var bad atomic.Int64
	var readers sync.WaitGroup
	for range 4 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for !stop.Load() {
				if a := LoadAuth(); !a.LoggedIn || a.Active.UserID != "u1" {
					bad.Add(1)
					t.Logf("LoadAuth: %+v", a)
				}
				if _, err := ServerClient("u1"); err != nil {
					bad.Add(1)
					t.Logf("ServerClient: %v", err)
				}
			}
		}()
	}
	var writers sync.WaitGroup
	for range 3 {
		writers.Add(1)
		go func() {
			defer writers.Done()
			for range 150 {
				write()
				if err := ClearSavedExitNode(); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	writers.Wait()
	stop.Store(true)
	readers.Wait()
	if n := bad.Load(); n > 0 {
		t.Fatalf("%d reads saw a missing or torn accounts file", n)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".accounts-") {
			t.Errorf("leftover temp file %s", e.Name())
		}
	}
}
