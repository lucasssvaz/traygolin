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

package pangolin

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// CLIConfig is the subset of the CLI's config.json shown in Preferences.
type CLIConfig struct {
	LogFile                 string
	TunnelDNS               bool
	OverrideDNS             bool
	UpstreamDNS             []string
	MatchDomains            []string
	PreferLocalRoutes       bool
	ExitNodeTakesPrecedence bool
}

// Equal reports whether two configs are the same.
func (c CLIConfig) Equal(o CLIConfig) bool {
	return c.LogFile == o.LogFile &&
		c.TunnelDNS == o.TunnelDNS &&
		c.OverrideDNS == o.OverrideDNS &&
		slices.Equal(c.UpstreamDNS, o.UpstreamDNS) &&
		slices.Equal(c.MatchDomains, o.MatchDomains) &&
		c.PreferLocalRoutes == o.PreferLocalRoutes &&
		c.ExitNodeTakesPrecedence == o.ExitNodeTakesPrecedence
}

// LoadConfig reads config.json without spawning the CLI. Missing keys
// take the CLI's defaults.
func LoadConfig() CLIConfig {
	data, err := os.ReadFile(configPath())
	if err != nil {
		return parseCLIConfig(nil)
	}
	return parseCLIConfig(data)
}

// sessionCookieName is the CLI's session_cookie_name override, if any.
func sessionCookieName() string {
	data, err := os.ReadFile(configPath())
	if err != nil {
		return ""
	}
	var cfg struct {
		Name string `json:"session_cookie_name"`
	}
	_ = json.Unmarshal(data, &cfg)
	return cfg.Name
}

func parseCLIConfig(raw []byte) CLIConfig {
	cfg := CLIConfig{OverrideDNS: true, LogFile: DefaultLogPath()}
	var payload map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &payload) != nil {
		return cfg
	}
	if s, ok := payload["log_file"].(string); ok && s != "" {
		cfg.LogFile = s
	}
	up, _ := payload["up"].(map[string]any)
	get := func(key string) (any, bool) {
		if v, ok := payload["up."+key]; ok {
			return v, true
		}
		v, ok := up[key]
		return v, ok
	}
	if v, ok := get("tunnel_dns"); ok {
		cfg.TunnelDNS = asBool(v)
	}
	if v, ok := get("override_dns"); ok {
		cfg.OverrideDNS = asBool(v)
	}
	if v, ok := get("prefer_local_routes"); ok {
		cfg.PreferLocalRoutes = asBool(v)
	}
	if v, ok := get("exit_node_takes_precedence"); ok {
		cfg.ExitNodeTakesPrecedence = asBool(v)
	}
	if v, ok := get("upstream_dns"); ok {
		cfg.UpstreamDNS = asStrings(v)
	}
	if v, ok := get("match_domains_dns"); ok {
		cfg.MatchDomains = asStrings(v)
	}
	return cfg
}

func asStrings(v any) []string {
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	case string:
		return SplitCSV(t)
	}
	return nil
}

// SplitCSV splits a comma-separated list, dropping blanks.
func SplitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func asBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		b, err := strconv.ParseBool(t)
		return err == nil && b
	}
	return false
}

// ConfigSet runs `pangolin config set key value`.
func (c *Client) ConfigSet(ctx context.Context, key, value string) error {
	return c.run(ctx, 10*time.Second, "config", "set", key, value)
}
