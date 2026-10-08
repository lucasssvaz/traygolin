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

package privhelper

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type flagKind int

const (
	valueFlag flagKind = iota
	boolFlag
)

type flagSpec struct {
	kind  flagKind
	check func(string) error
}

var (
	reIdent     = regexp.MustCompile(`^[A-Za-z0-9._:@-]{1,256}$`)
	reIface     = regexp.MustCompile(`^[A-Za-z0-9_-]{1,15}$`)
	reHostPort  = regexp.MustCompile(`^[A-Za-z0-9.:\[\]-]{1,256}$`)
	reDomainPat = regexp.MustCompile(`^[\p{L}\p{N}.*?_-]{1,253}$`)
	reInt       = regexp.MustCompile(`^[0-9]{1,10}$`)
)

func matchRe(re *regexp.Regexp, what string) func(string) error {
	return func(v string) error {
		if !re.MatchString(v) {
			return fmt.Errorf("invalid %s %q", what, v)
		}
		return nil
	}
}

func checkEndpoint(v string) error {
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("invalid endpoint %q", v)
	}
	return nil
}

func checkSecret(v string) error {
	if v == "" || len(v) > 512 || strings.ContainsAny(v, "\x00\r\n") {
		return fmt.Errorf("invalid secret")
	}
	return nil
}

func checkIntRange(lo, hi int) func(string) error {
	return func(v string) error {
		n, err := strconv.Atoi(v)
		if err != nil || n < lo || n > hi {
			return fmt.Errorf("value %q out of range %d-%d", v, lo, hi)
		}
		return nil
	}
}

func checkDuration(v string) error {
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 || d > time.Hour {
		return fmt.Errorf("invalid duration %q", v)
	}
	return nil
}

// maxListItems bounds the comma-separated flags. The CLI sets no limit and
// forwards the lists from config.json whole, so this is only a backstop.
const maxListItems = 256

func checkList(item *regexp.Regexp, what string) func(string) error {
	return func(v string) error {
		parts := strings.Split(v, ",")
		if len(parts) > maxListItems {
			return fmt.Errorf("too many %s entries", what)
		}
		for _, p := range parts {
			if !item.MatchString(strings.TrimSpace(p)) {
				return fmt.Errorf("invalid %s %q", what, p)
			}
		}
		return nil
	}
}

func checkLogLevel(v string) error {
	if v != "debug" && v != "info" {
		return fmt.Errorf("invalid log level %q", v)
	}
	return nil
}

// allowed lists every `pangolin up client` flag the helper will pass to
// the root process. Flags that read files or open listeners as root
// (--tls-client-cert, --http-addr) and --attach/--subnet-router are
// deliberately absent.
var allowed = map[string]flagSpec{
	"org":                        {valueFlag, matchRe(reIdent, "organization")},
	"id":                         {valueFlag, matchRe(reIdent, "client ID")},
	"secret":                     {valueFlag, checkSecret},
	"endpoint":                   {valueFlag, checkEndpoint},
	"mtu":                        {valueFlag, checkIntRange(576, 9000)},
	"netstack-dns":               {valueFlag, matchRe(reHostPort, "DNS server")},
	"interface-name":             {valueFlag, matchRe(reIface, "interface name")},
	"log-level":                  {valueFlag, checkLogLevel},
	"ping-interval":              {valueFlag, checkDuration},
	"ping-timeout":               {valueFlag, checkDuration},
	"upstream-dns":               {valueFlag, checkList(reHostPort, "DNS server")},
	"match-domains":              {valueFlag, checkList(reDomainPat, "match domain")},
	"exit-node-site-ids":         {valueFlag, checkList(reInt, "site ID")},
	"exit-node-resource-id":      {valueFlag, checkIntRange(1, 1<<31-1)},
	"holepunch":                  {kind: boolFlag},
	"override-dns":               {kind: boolFlag},
	"tunnel-dns":                 {kind: boolFlag},
	"prefer-local-routes":        {kind: boolFlag},
	"exit-node-takes-precedence": {kind: boolFlag},
	"disable-relay":              {kind: boolFlag},
}

// ValidateUpArgs checks arguments destined for `pangolin up client` run
// as root. It accepts only "up client" followed by allowed flags.
func ValidateUpArgs(args []string) error {
	if len(args) < 2 || args[0] != "up" || args[1] != "client" {
		return fmt.Errorf("only `up client` is allowed")
	}
	seen := map[string]bool{}
	for i := 2; i < len(args); i++ {
		arg := args[i]
		if strings.ContainsRune(arg, 0) {
			return fmt.Errorf("NUL byte in argument")
		}
		if !strings.HasPrefix(arg, "--") {
			return fmt.Errorf("unexpected positional argument %q", arg)
		}
		name, value, hasValue := strings.Cut(arg[2:], "=")
		spec, ok := allowed[name]
		if !ok {
			return fmt.Errorf("flag --%s is not allowed", name)
		}
		if seen[name] {
			return fmt.Errorf("flag --%s given twice", name)
		}
		seen[name] = true
		switch spec.kind {
		case boolFlag:
			if hasValue {
				if _, err := strconv.ParseBool(value); err != nil {
					return fmt.Errorf("invalid boolean for --%s: %q", name, value)
				}
			}
		case valueFlag:
			if !hasValue {
				i++
				if i >= len(args) {
					return fmt.Errorf("flag --%s needs a value", name)
				}
				value = args[i]
			}
			if err := spec.check(value); err != nil {
				return fmt.Errorf("--%s: %w", name, err)
			}
		}
	}
	for _, req := range []string{"id", "secret", "endpoint"} {
		if !seen[req] {
			return fmt.Errorf("missing required flag --%s", req)
		}
	}
	return nil
}
