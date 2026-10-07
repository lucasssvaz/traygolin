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

package main

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/lucasssvaz/traygolin/internal/privhelper"
	"github.com/lucasssvaz/traygolin/internal/ui"
)

func main() {
	// The Pangolin CLI elevates by running `sudo`. When Traygolin starts it,
	// a symlink named sudo points back here and is handled without GTK.
	if filepath.Base(os.Args[0]) == "sudo" {
		os.Exit(privhelper.RunShim(os.Args[1:], os.Stderr))
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go func() {
		<-ctx.Done()
		time.Sleep(3 * time.Second)
		os.Exit(0)
	}()

	var a ui.App
	os.Exit(a.Run(ctx))
}
