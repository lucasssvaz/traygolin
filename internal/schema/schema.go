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

package schema

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/lucasssvaz/traygolin/internal/metadata"
)

func Prepare(xml []byte) (string, error) {
	var dirs []string
	if c, err := os.UserCacheDir(); err == nil {
		dirs = append(dirs, filepath.Join(c, metadata.AppID, "glib-2.0", "schemas"))
	}
	dirs = append(dirs, filepath.Join(os.TempDir(), metadata.AppID, "glib-2.0", "schemas"))
	var last error
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			last = err
			continue
		}
		path := filepath.Join(dir, metadata.AppID+".gschema.xml")
		if err := os.WriteFile(path, xml, 0o644); err != nil {
			last = err
			continue
		}
		cmd := exec.Command("glib-compile-schemas", dir)
		if out, err := cmd.CombinedOutput(); err != nil {
			last = fmt.Errorf("glib-compile-schemas: %w: %s", err, out)
			continue
		}
		if err := os.Setenv("GSETTINGS_SCHEMA_DIR", dir); err != nil {
			return "", err
		}
		return dir, nil
	}
	if last == nil {
		last = fmt.Errorf("no schema directory available")
	}
	return "", last
}
