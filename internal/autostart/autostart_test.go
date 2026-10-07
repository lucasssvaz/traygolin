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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setup(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("PATH", "")
	return filepath.Join(dir, "autostart", desktopName)
}

func read(t *testing.T, p string) string {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSetAndEnabled(t *testing.T) {
	p := setup(t)
	if Enabled() {
		t.Fatal("expected disabled")
	}
	if err := Set(true, "/usr/bin/traygolin"); err != nil {
		t.Fatal(err)
	}
	if !Enabled() {
		t.Fatal("expected enabled")
	}
	body := read(t, p)
	for _, want := range []string{"\nExec=/usr/bin/traygolin --hide-window\n", "\nTryExec=/usr/bin/traygolin\n"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in\n%s", want, body)
		}
	}
	if err := Set(false, ""); err != nil {
		t.Fatal(err)
	}
	if Enabled() {
		t.Fatal("expected removed")
	}
	if err := Set(false, ""); err != nil {
		t.Fatal("removing twice:", err)
	}
}

func TestSetRewritesMovedExecutable(t *testing.T) {
	p := setup(t)
	if err := Set(true, "/home/me/src/traygolin/bin/traygolin"); err != nil {
		t.Fatal(err)
	}
	if err := Set(true, "/usr/bin/traygolin"); err != nil {
		t.Fatal(err)
	}
	if body := read(t, p); !strings.Contains(body, "\nExec=/usr/bin/traygolin --hide-window\n") {
		t.Fatalf("stale entry:\n%s", body)
	}
}

func TestSetPrefersNameOnPath(t *testing.T) {
	p := setup(t)
	bin := t.TempDir()
	exe := filepath.Join(bin, "traygolin")
	if err := os.WriteFile(exe, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	if err := Set(true, exe); err != nil {
		t.Fatal(err)
	}
	body := read(t, p)
	if !strings.Contains(body, "\nExec=traygolin --hide-window\n") || !strings.Contains(body, "\nTryExec=traygolin\n") {
		t.Fatalf("expected bare name:\n%s", body)
	}

	other := filepath.Join(t.TempDir(), "traygolin")
	if err := os.WriteFile(other, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Set(true, other); err != nil {
		t.Fatal(err)
	}
	if body := read(t, p); !strings.Contains(body, "\nExec="+other+" --hide-window\n") {
		t.Fatalf("a different binary on PATH must not be used:\n%s", body)
	}
}

func TestSetQuotesExec(t *testing.T) {
	p := setup(t)
	if err := Set(true, `/opt/My Apps/$x/traygolin`); err != nil {
		t.Fatal(err)
	}
	body := read(t, p)
	want := `Exec="/opt/My Apps/\\$x/traygolin" --hide-window`
	if !strings.Contains(body, "\n"+want+"\n") {
		t.Fatalf("want %s in\n%s", want, body)
	}
	if !strings.Contains(body, "\nTryExec=/opt/My Apps/$x/traygolin\n") {
		t.Fatalf("TryExec:\n%s", body)
	}
}
