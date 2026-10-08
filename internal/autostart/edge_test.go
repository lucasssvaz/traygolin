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

package autostart

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// unescapeValue undoes the Desktop Entry string escapes (\s \n \t \r \\).
func unescapeValue(s string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			continue
		}
		i++
		if i >= len(s) {
			return "", fmt.Errorf("dangling backslash")
		}
		switch s[i] {
		case 's':
			b.WriteByte(' ')
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case '\\':
			b.WriteByte('\\')
		default:
			return "", fmt.Errorf("unknown escape \\%c", s[i])
		}
	}
	return b.String(), nil
}

// parseExec splits an unescaped Exec value the way the Desktop Entry
// specification says a launcher must: double quotes group, inside them only
// \" \` \$ \\ are escapes, outside them the reserved characters are not
// allowed, and %% is a literal percent sign. Any other field code is
// reported, because Traygolin does not use any.
func parseExec(s string) ([]string, error) {
	const reserved = "\t\n\"'\\><~|&;$*?#()`"
	var args []string
	var cur strings.Builder
	have, inQuote := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inQuote && c == '\\':
			i++
			if i >= len(s) || !strings.ContainsRune("\"`$\\", rune(s[i])) {
				return nil, fmt.Errorf("bad escape in quotes at %d", i)
			}
			cur.WriteByte(s[i])
		case inQuote && c == '"':
			inQuote = false
		case inQuote:
			if c == '%' {
				if i+1 >= len(s) || s[i+1] != '%' {
					return nil, fmt.Errorf("field code at %d", i)
				}
				i++
			}
			cur.WriteByte(c)
		case c == '"':
			inQuote, have = true, true
		case c == ' ':
			if have {
				args = append(args, cur.String())
				cur.Reset()
				have = false
			}
		case strings.IndexByte(reserved, c) >= 0:
			return nil, fmt.Errorf("unquoted reserved character %q at %d", c, i)
		case c == '%':
			if i+1 >= len(s) || s[i+1] != '%' {
				return nil, fmt.Errorf("field code at %d", i)
			}
			i++
			cur.WriteByte('%')
			have = true
		default:
			cur.WriteByte(c)
			have = true
		}
	}
	if inQuote {
		return nil, fmt.Errorf("unterminated quote")
	}
	if have {
		args = append(args, cur.String())
	}
	return args, nil
}

func entryValue(t *testing.T, body, key string) string {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		if v, ok := strings.CutPrefix(line, key+"="); ok {
			return v
		}
	}
	t.Fatalf("no %s line in\n%s", key, body)
	return ""
}

func TestExecSurvivesAnyPath(t *testing.T) {
	for _, path := range []string{
		"/usr/bin/traygolin",
		"/opt/My Apps/traygolin",
		"/home/jörg/bin/traygolin",
		"/home/日本語/tray golin",
		`/opt/with"quote/traygolin`,
		"/opt/with'apostrophe/traygolin",
		`/opt/back\slash/traygolin`,
		`/opt/trailing\`,
		"/opt/$HOME/traygolin",
		"/opt/`cmd`/traygolin",
		"/opt/100%/traygolin",
		"/opt/%f/traygolin",
		"/opt/%%/traygolin",
		"/opt/%u %U/traygolin",
		"/opt/a&b;c|d/traygolin",
		"/opt/(paren)/traygolin",
		"/opt/<angle>/traygolin",
		"/opt/~tilde/traygolin",
		"/opt/#hash/traygolin",
		"/opt/star*?/traygolin",
		"/opt/tab\there/traygolin",
		"/opt/new\nline/traygolin",
		"/opt/cr\rhere/traygolin",
		"/opt/equals=sign/traygolin",
		"/opt/semi;colon/traygolin",
		"/opt/  leading and trailing  /traygolin",
		"relative/path",
		"-dash",
	} {
		t.Run(fmt.Sprintf("%q", path), func(t *testing.T) {
			p := setup(t)
			if err := Set(true, path); err != nil {
				t.Fatal(err)
			}
			body := read(t, p)
			for _, line := range strings.Split(body, "\n") {
				if strings.ContainsAny(line, "\r") {
					t.Fatalf("raw control character in a line: %q", line)
				}
			}
			raw, err := unescapeValue(entryValue(t, body, "Exec"))
			if err != nil {
				t.Fatal(err)
			}
			args, err := parseExec(raw)
			if err != nil {
				t.Fatalf("a launcher cannot parse %q: %v", raw, err)
			}
			if len(args) != 2 || args[0] != path || args[1] != "--hide-window" {
				t.Fatalf("launcher would run %q\nfor Exec=%s", args, entryValue(t, body, "Exec"))
			}
			try, err := unescapeValue(entryValue(t, body, "TryExec"))
			if err != nil || try != path {
				t.Fatalf("TryExec = %q (%v)", try, err)
			}
			// Every line must still be a valid key=value line, so a path with
			// a newline cannot inject another key.
			for _, line := range strings.Split(strings.TrimSuffix(body, "\n"), "\n") {
				if line != "" && !strings.HasPrefix(line, "[") && !strings.Contains(line, "=") {
					t.Fatalf("stray line %q in\n%s", line, body)
				}
			}
			if strings.Count(body, "\nExec=") != 1 {
				t.Fatalf("injected a second Exec key:\n%s", body)
			}
		})
	}
}

func TestEntryHasNoInjectableKeys(t *testing.T) {
	body := entry("/x\nExec=/bin/evil\n[Desktop Action x]\nExec=/bin/evil")
	if strings.Count(body, "\nExec=") != 1 || strings.Contains(body, "\n[Desktop Action") {
		t.Fatalf("path injected content:\n%s", body)
	}
}

func TestEntryRequiredKeys(t *testing.T) {
	body := entry("traygolin")
	for _, want := range []string{"[Desktop Entry]\n", "\nType=Application\n", "\nName=Traygolin\n", "\nExec=", "\nIcon=io.github.lucasssvaz.Traygolin\n", "\nTerminal=false\n"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	if !strings.HasSuffix(body, "\n") {
		t.Error("a desktop file ends with a newline")
	}
	if strings.HasPrefix(body, "\n") || !strings.HasPrefix(body, "[Desktop Entry]") {
		t.Error("the group header must come first")
	}
}

func TestSetFileProperties(t *testing.T) {
	p := setup(t)
	if err := Set(true, "/usr/bin/traygolin"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o022 != 0 {
		t.Errorf("autostart file is writable by others: %v", info.Mode())
	}
	if info.Mode().Perm()&0o400 == 0 {
		t.Errorf("autostart file is not readable: %v", info.Mode())
	}
}

func TestSetIsIdempotent(t *testing.T) {
	p := setup(t)
	for range 3 {
		if err := Set(true, "/usr/bin/traygolin"); err != nil {
			t.Fatal(err)
		}
	}
	first := read(t, p)
	st1, _ := os.Stat(p)
	if err := Set(true, "/usr/bin/traygolin"); err != nil {
		t.Fatal(err)
	}
	st2, _ := os.Stat(p)
	if first != read(t, p) || !st1.ModTime().Equal(st2.ModTime()) {
		t.Error("an unchanged entry should not be rewritten")
	}
	for range 3 {
		if err := Set(false, ""); err != nil {
			t.Fatal(err)
		}
	}
	if Enabled() {
		t.Error("still enabled")
	}
}

func TestDisableWithoutDirectory(t *testing.T) {
	setup(t)
	if err := Set(false, "/x"); err != nil {
		t.Fatalf("disabling when nothing was ever enabled: %v", err)
	}
}

func TestEnableCreatesNestedDirectories(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "does", "not", "exist"))
	t.Setenv("PATH", "")
	if err := Set(true, "/usr/bin/traygolin"); err != nil {
		t.Fatal(err)
	}
	if !Enabled() {
		t.Error("not enabled")
	}
}

func TestHomeFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", home)
	t.Setenv("PATH", "")
	if err := Set(true, "/usr/bin/traygolin"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "autostart", desktopName)); err != nil {
		t.Errorf("not written under ~/.config: %v", err)
	}
	t.Setenv("HOME", "")
	if Enabled() {
		t.Error("without a home directory nothing can be enabled")
	}
	if err := Set(true, "/usr/bin/traygolin"); err == nil {
		t.Error("expected an error without a home directory")
	}
}

func TestEnabledFalseForBrokenPaths(t *testing.T) {
	p := setup(t)
	if Enabled() {
		t.Fatal("nothing there yet")
	}
	// A dangling symlink is not an enabled entry.
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/nonexistent/target", p); err != nil {
		t.Fatal(err)
	}
	if Enabled() {
		t.Error("a dangling link counts as enabled")
	}
}

func TestSetReplacesAStaleEntry(t *testing.T) {
	p := setup(t)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Set(true, "/usr/bin/traygolin"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(read(t, p), "[Desktop Entry]") {
		t.Error("stale entry kept")
	}
}

func TestSetFailsCleanlyWhenTheDirectoryIsReadOnly(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores permissions")
	}
	p := setup(t)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(p), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Dir(p), 0o755) })
	if err := Set(true, "/usr/bin/traygolin"); err == nil {
		t.Error("expected a permission error")
	}
	if Enabled() {
		t.Error("a failed write left an entry")
	}
}

func TestSetWhenTheEntryIsADirectory(t *testing.T) {
	p := setup(t)
	if err := os.MkdirAll(filepath.Join(p, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Set(true, "/usr/bin/traygolin"); err == nil {
		t.Error("expected an error")
	}
	if err := Set(false, ""); err == nil {
		t.Error("removing a non-empty directory should be reported, not hidden")
	}
}

func TestCommandForEmptyPath(t *testing.T) {
	setup(t)
	if got := command(""); got != "traygolin" {
		t.Errorf("%q", got)
	}
}

func TestSetWithEmptyPathUsesTheName(t *testing.T) {
	p := setup(t)
	if err := Set(true, ""); err != nil {
		t.Fatal(err)
	}
	if body := read(t, p); !strings.Contains(body, "\nExec=traygolin --hide-window\n") {
		t.Errorf("%s", body)
	}
}

func TestConcurrentSetNeverLeavesAPartialFile(t *testing.T) {
	p := setup(t)
	var wg sync.WaitGroup
	stop := make(chan struct{})
	bad := make(chan string, 1)
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			data, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			s := string(data)
			if !strings.HasPrefix(s, "[Desktop Entry]") || !strings.HasSuffix(s, "\n") {
				select {
				case bad <- s:
				default:
				}
				return
			}
		}
	}()
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range 50 {
				path := fmt.Sprintf("/opt/app%d-%d/traygolin", i, j%3)
				if (i+j)%5 == 0 {
					Set(false, "")
				} else if err := Set(true, path); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	close(stop)
	select {
	case s := <-bad:
		t.Fatalf("a reader saw a partial entry (autostart is read while the app saves settings):\n%q", s)
	default:
	}
}

func FuzzEntryExec(f *testing.F) {
	for _, s := range []string{"/usr/bin/traygolin", "/a b/c", `/q"uote`, "/p%f", "/new\nline", `\`, ""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, path string) {
		if path == "" || strings.ContainsRune(path, 0) || !validUTF8(path) {
			return
		}
		body := entry(path)
		raw, err := unescapeValue(entryValue(t, body, "Exec"))
		if err != nil {
			t.Fatal(err)
		}
		args, err := parseExec(raw)
		if err != nil || len(args) != 2 || args[0] != path || args[1] != "--hide-window" {
			t.Fatalf("path %q -> Exec %q -> %q (%v)", path, raw, args, err)
		}
		if strings.Count(body, "\nExec=") != 1 {
			t.Fatalf("Exec key injected: %q", body)
		}
	})
}

func validUTF8(s string) bool { return strings.ToValidUTF8(s, "") == s }
