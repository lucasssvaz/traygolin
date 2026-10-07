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

package metadata

const (
	AppID      = "io.github.lucasssvaz.Traygolin"
	AppName    = "Traygolin"
	Website    = "https://github.com/lucasssvaz/traygolin"
	CLIInstall = "curl -fsSL https://static.pangolin.net/get-cli.sh | bash"
	CloudHost  = "https://app.pangolin.net"
)

// Version is set at build time with -X.
var Version = "0.1.0"
