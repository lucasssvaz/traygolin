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

func TestSetAndEnabled(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if Enabled() {
		t.Fatal("expected disabled")
	}
	if err := Set(true, "/usr/bin/traygolin"); err != nil {
		t.Fatal(err)
	}
	if !Enabled() {
		t.Fatal("expected enabled")
	}
	data, err := os.ReadFile(filepath.Join(dir, "autostart", desktopName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Exec=/usr/bin/traygolin --hide-window") {
		t.Fatalf("%s", data)
	}
	if err := Set(true, ""); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(filepath.Join(dir, "autostart", desktopName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Exec=traygolin --hide-window") {
		t.Fatalf("%s", data)
	}
	if err := Set(false, ""); err != nil {
		t.Fatal(err)
	}
	if Enabled() {
		t.Fatal("expected removed")
	}
}
