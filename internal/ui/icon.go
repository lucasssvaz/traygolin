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

package ui

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	traygolin "github.com/lucasssvaz/traygolin"
	"github.com/lucasssvaz/traygolin/internal/metadata"
)

// prepareIcon makes the app icon resolvable by name when Traygolin is not
// installed system-wide. GTK treats files placed directly in an icon
// search path as unthemed icons, which an installed hicolor icon still
// takes precedence over. It returns the path of the cached PNG, or "".
func prepareIcon() string {
	path, err := cacheIcon()
	if err != nil {
		slog.Warn("cache app icon", "err", err)
	}
	if path != "" {
		if d := gdk.DisplayGetDefault(); d != nil {
			gtk.IconThemeGetForDisplay(d).AddSearchPath(filepath.Dir(path))
		}
	}
	gtk.WindowSetDefaultIconName(metadata.AppID)
	return path
}

func cacheIcon() (string, error) {
	c, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(c, metadata.AppID, "icons")
	path := filepath.Join(dir, metadata.AppID+".png")
	if cur, err := os.ReadFile(path); err == nil && bytes.Equal(cur, traygolin.IconPNG) {
		return path, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".icon-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(traygolin.IconPNG); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return "", err
	}
	return path, os.Rename(tmp.Name(), path)
}

// notificationIcon uses the cached file because notification servers run
// in another process and cannot see GTK's search path.
func notificationIcon(path string) gio.Iconner {
	if path != "" {
		return gio.NewFileIcon(gio.NewFileForPath(path))
	}
	icon, err := gio.NewIconForString(metadata.AppID)
	if err != nil {
		return nil
	}
	return icon
}
