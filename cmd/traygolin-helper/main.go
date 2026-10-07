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

// Command traygolin-helper starts and stops the Pangolin tunnel as root.
// It is run only through pkexec under the io.github.lucasssvaz.Traygolin.tunnel
// polkit action.
package main

import (
	"os"

	"github.com/lucasssvaz/traygolin/internal/privhelper"
)

func main() {
	os.Exit(privhelper.HelperMain(os.Args[1:], os.Stdin, os.Stderr))
}
