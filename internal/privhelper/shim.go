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

// Package privhelper lets `pangolin up` start its root tunnel process
// through polkit instead of an interactive sudo prompt.
//
// The Pangolin CLI elevates by running
//
//	sudo sh -c 'export PANGOLIN_SUBPROCESS=1 && [export PANGOLIN_CREDENTIALS_FROM_KEYRING=1 && ]nohup "exe" "up" "client" ... >/dev/null 2>&1 &'
//
// Traygolin puts a "sudo" shim first in PATH for that one invocation.
// The shim parses exactly that shape and forwards the arguments to a
// small root helper via pkexec, which validates them again.
package privhelper

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	envSubprocess = "PANGOLIN_SUBPROCESS"
	envKeyring    = "PANGOLIN_CREDENTIALS_FROM_KEYRING"
	shellSuffix   = ">/dev/null 2>&1 &"
)

// Launch is a parsed request to start the tunnel process as root.
type Launch struct {
	// Keyring means the CLI took credentials from the user's saved
	// account, so the root process should load the session token too.
	Keyring bool
	// Exe is the pangolin path the CLI asked for. The helper ignores it
	// and runs a trusted system binary instead.
	Exe  string
	Args []string
}

// ErrUnsupported is returned for any sudo invocation the shim does not
// recognise. The shim never falls back to the real sudo.
var ErrUnsupported = errors.New("traygolin: unsupported privileged command from the pangolin CLI")

// ParseSudo parses the arguments the CLI passed to sudo.
func ParseSudo(args []string) (Launch, error) {
	if len(args) != 3 || args[0] != "sh" || args[1] != "-c" {
		return Launch{}, fmt.Errorf("%w: sudo %s", ErrUnsupported, strings.Join(args, " "))
	}
	return ParseShell(args[2])
}

// ParseShell parses the `sh -c` script the CLI builds for detached mode.
func ParseShell(script string) (Launch, error) {
	var l Launch
	rest := strings.TrimSpace(script)

	prefix := "export " + envSubprocess + "=1 && "
	if !strings.HasPrefix(rest, prefix) {
		return l, fmt.Errorf("%w: missing %s", ErrUnsupported, envSubprocess)
	}
	rest = rest[len(prefix):]

	keyring := "export " + envKeyring + "=1 && "
	if strings.HasPrefix(rest, keyring) {
		l.Keyring = true
		rest = rest[len(keyring):]
	}

	if !strings.HasPrefix(rest, "nohup ") {
		return l, fmt.Errorf("%w: missing nohup", ErrUnsupported)
	}
	rest = rest[len("nohup "):]

	if !strings.HasSuffix(rest, shellSuffix) {
		return l, fmt.Errorf("%w: missing background redirect", ErrUnsupported)
	}
	rest = strings.TrimSpace(strings.TrimSuffix(rest, shellSuffix))

	words, err := splitQuoted(rest)
	if err != nil {
		return l, err
	}
	if len(words) < 3 {
		return l, fmt.Errorf("%w: too few arguments", ErrUnsupported)
	}
	l.Exe = words[0]
	l.Args = words[1:]
	return l, nil
}

// splitQuoted splits a sequence of Go-quoted strings separated by spaces,
// the format produced by fmt's %q verb.
func splitQuoted(s string) ([]string, error) {
	var out []string
	for i := 0; i < len(s); {
		if s[i] == ' ' {
			i++
			continue
		}
		if s[i] != '"' {
			return nil, fmt.Errorf("%w: unquoted word at offset %d", ErrUnsupported, i)
		}
		j := i + 1
		for ; j < len(s); j++ {
			if s[j] == '\\' {
				j++
				continue
			}
			if s[j] == '"' {
				break
			}
		}
		if j >= len(s) {
			return nil, fmt.Errorf("%w: unterminated quote", ErrUnsupported)
		}
		word, err := strconv.Unquote(s[i : j+1])
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUnsupported, err)
		}
		out = append(out, word)
		i = j + 1
		if i < len(s) && s[i] != ' ' {
			return nil, fmt.Errorf("%w: junk after quoted word", ErrUnsupported)
		}
	}
	return out, nil
}
