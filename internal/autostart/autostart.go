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
	"os/exec"
	"path/filepath"
	"strings"
)

const desktopName = "io.github.lucasssvaz.Traygolin.desktop"

func dir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "autostart"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "autostart"), nil
}

func path() (string, error) {
	d, err := dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, desktopName), nil
}

func Enabled() bool {
	p, err := path()
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

// Set installs or removes the autostart entry. An existing entry is
// rewritten only if it differs, so calling Set on every start keeps the
// launch command current when the executable moves.
func Set(enabled bool, execPath string) error {
	p, err := path()
	if err != nil {
		return err
	}
	if !enabled {
		err := os.Remove(p)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	body := entry(command(execPath))
	if old, err := os.ReadFile(p); err == nil && string(old) == body {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return writeAtomic(p, []byte(body), 0o644)
}

// command prefers the bare name when PATH resolves it to execPath, so a
// packaged install keeps working if its location changes.
func command(execPath string) string {
	if execPath == "" {
		return "traygolin"
	}
	found, err := exec.LookPath("traygolin")
	if err != nil {
		return execPath
	}
	a, errA := os.Stat(found)
	b, errB := os.Stat(execPath)
	if errA == nil && errB == nil && os.SameFile(a, b) {
		return "traygolin"
	}
	return execPath
}

// TryExec makes desktops skip the entry once the binary is gone.
func entry(cmd string) string {
	return strings.Join([]string{
		"[Desktop Entry]",
		"Type=Application",
		"Name=Traygolin",
		"Comment=Unofficial Pangolin Linux GUI",
		"TryExec=" + escapeString(cmd),
		"Exec=" + escapeString(quoteArg(cmd)) + " --hide-window",
		"Icon=io.github.lucasssvaz.Traygolin",
		"Terminal=false",
		"X-GNOME-Autostart-enabled=true",
		"",
	}, "\n")
}

// quoteArg quotes an Exec argument per the Desktop Entry spec.
func quoteArg(s string) string {
	// A percent sign starts a field code (%f, %u, ...) in Exec, so a literal
	// one has to be doubled.
	s = strings.ReplaceAll(s, "%", "%%")
	if !strings.ContainsAny(s, " \t\n\"'\\><~|&;$*?#()`") {
		return s
	}
	r := strings.NewReplacer(`"`, `\"`, "`", "\\`", `$`, `\$`, `\`, `\\`)
	return `"` + r.Replace(s) + `"`
}

// escapeString applies the escapes of the spec's string value type.
func escapeString(s string) string {
	return strings.NewReplacer(`\`, `\\`, "\n", `\n`, "\t", `\t`, "\r", `\r`).Replace(s)
}

// writeAtomic replaces path in one step, so that a desktop session starting
// at the same moment never reads half an entry.
func writeAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".autostart-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
