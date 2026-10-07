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

package traygolin

import _ "embed"

//go:embed io.github.lucasssvaz.Traygolin.gschema.xml
var SchemaXML []byte

//go:embed io.github.lucasssvaz.Traygolin.png
var IconPNG []byte

//go:embed io.github.lucasssvaz.Traygolin-tray.png
var TrayIconPNG []byte

//go:embed io.github.lucasssvaz.Traygolin-tray-connected.png
var TrayConnectedPNG []byte

//go:embed io.github.lucasssvaz.Traygolin-tray-exit.png
var TrayExitPNG []byte

// PolicyTemplate is the polkit action with an @HELPER@ placeholder.
//
//go:embed io.github.lucasssvaz.Traygolin.policy.in
var PolicyTemplate []byte
