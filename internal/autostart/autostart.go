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
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if execPath == "" {
		execPath = "traygolin"
	}
	body := strings.Join([]string{
		"[Desktop Entry]",
		"Type=Application",
		"Name=Traygolin",
		"Comment=Unofficial Pangolin Linux GUI",
		"Exec=" + execPath + " --hide-window",
		"Icon=io.github.lucasssvaz.Traygolin",
		"Terminal=false",
		"X-GNOME-Autostart-enabled=true",
		"",
	}, "\n")
	return os.WriteFile(p, []byte(body), 0o644)
}
