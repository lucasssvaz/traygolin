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

// Package pangolin wraps the user-level commands of the official Pangolin
// CLI and reads its on-disk account and config files.
package pangolin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const defaultBinary = "pangolin"

// Client invokes the official Pangolin CLI.
type Client struct {
	Binary string
	// Env is appended to the environment of every invocation.
	Env []string
}

func (c *Client) bin() string {
	if c != nil && c.Binary != "" {
		return c.Binary
	}
	if env := os.Getenv("PANGOLIN_BINARY"); env != "" {
		return env
	}
	return defaultBinary
}

// LookPath reports where the CLI is, or an error if it is missing.
func (c *Client) LookPath() (string, error) {
	return exec.LookPath(c.bin())
}

// Version returns the CLI version string. `pangolin version` may follow
// it with an update notice, which is dropped.
func (c *Client) Version(ctx context.Context) (string, error) {
	out, err := c.output(ctx, 3*time.Second, nil, "version")
	if err != nil {
		return "", err
	}
	v, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
	return strings.TrimSpace(v), nil
}

// olmEnv are the variables `pangolin up` reads in place of its own flags,
// under the generic names the standalone olm client uses. A desktop session
// can carry any of them for unrelated reasons (LOG_LEVEL=warn, INTERFACE=
// wlan0), and the CLI forwards them to the root tunnel as flags. Some of
// those the helper refuses, which fails the connection, and the rest would
// silently change the tunnel. Settings belong in Preferences or config.json.
var olmEnv = []string{
	"MTU", "DNS", "UPSTREAM_DNS", "MATCH_DOMAINS_DNS", "LOG_LEVEL", "INTERFACE",
	"HTTP_ADDR", "PING_INTERVAL", "PING_TIMEOUT", "OVERRIDE_DNS", "TUNNEL_DNS",
	"DISABLE_RELAY", "PREFER_LOCAL_ROUTES", "DISABLE_ROUTES_AND_ALIASES",
	"SUBNET_ROUTER", "DISABLE_HOLEPUNCH",
}

// baseEnv is the inherited environment for a CLI invocation.
func baseEnv(args []string) []string {
	env := os.Environ()
	if len(args) == 0 || args[0] != "up" {
		return env
	}
	return slices.DeleteFunc(env, func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")
		return slices.Contains(olmEnv, name)
	})
}

func (c *Client) command(ctx context.Context, extraEnv []string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, c.bin(), args...)
	cmd.Env = append(append(baseEnv(args), c.Env...), extraEnv...)
	cmd.Stdin = nil
	cmd.WaitDelay = time.Second
	return cmd
}

func (c *Client) output(ctx context.Context, timeout time.Duration, extraEnv []string, args ...string) (string, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	cmd := c.command(ctx, extraEnv, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := ignoreWaitDelay(cmd, cmd.Run())
	out := strings.TrimSpace(stdout.String())
	errText := strings.TrimSpace(stderr.String())
	if err != nil {
		return out, wrapRunError(c.bin(), args, out, errText, err)
	}
	if out == "" {
		return errText, nil
	}
	return out, nil
}

func (c *Client) run(ctx context.Context, timeout time.Duration, args ...string) error {
	_, err := c.output(ctx, timeout, nil, args...)
	return err
}

func (c *Client) stream(ctx context.Context, timeout time.Duration, onLine func(string), args ...string) (string, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	cmd := c.command(ctx, nil, args...)
	// Plain writers rather than pipes: exec then stops waiting for the output
	// WaitDelay after the CLI exits, even if a child it started (such as the
	// browser) still holds the pipe. Reading a pipe by hand would block until
	// that child quit.
	sink := &lineSink{onLine: onLine}
	stdout, stderr := &lineWriter{sink: sink}, &lineWriter{sink: sink}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Start(); err != nil {
		return "", wrapRunError(c.bin(), args, "", "", err)
	}
	waitErr := ignoreWaitDelay(cmd, cmd.Wait())
	stdout.flush()
	stderr.flush()
	out := strings.TrimSpace(sink.text())
	if waitErr != nil {
		return out, wrapRunError(c.bin(), args, out, "", waitErr)
	}
	return out, nil
}

// ignoreWaitDelay forgives the error exec reports when the CLI itself
// succeeded but a child it left behind kept the output open past WaitDelay.
func ignoreWaitDelay(cmd *exec.Cmd, err error) error {
	if errors.Is(err, exec.ErrWaitDelay) && cmd.ProcessState != nil && cmd.ProcessState.Success() {
		return nil
	}
	return err
}

// maxLine bounds how much of one output line is kept. The rest of an
// overlong line is dropped instead of failing the whole command.
const maxLine = 1024 * 1024

// maxKept bounds how much output a streamed command keeps for its result
// and error message. Older lines are dropped first.
const maxKept = 256 * 1024

// lineSink collects the lines of stdout and stderr in the order they arrive.
type lineSink struct {
	mu     sync.Mutex
	all    []byte
	onLine func(string)
}

func (s *lineSink) add(line string) {
	s.mu.Lock()
	s.all = append(append(s.all, line...), '\n')
	if len(s.all) > 2*maxKept {
		keep := s.all[len(s.all)-maxKept:]
		if i := bytes.IndexByte(keep, '\n'); i >= 0 && i+1 < len(keep) {
			keep = keep[i+1:]
		}
		s.all = append(s.all[:0], keep...)
	}
	s.mu.Unlock()
	if s.onLine != nil {
		s.onLine(line)
	}
}

func (s *lineSink) text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return string(s.all)
}

// lineWriter splits what it is given into lines. Each stream gets its own
// writer because lines from the two must not be glued together.
type lineWriter struct {
	sink    *lineSink
	mu      sync.Mutex
	pending []byte
	skip    bool // inside an overlong line
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	rest := p
	for len(rest) > 0 {
		i := bytes.IndexByte(rest, '\n')
		chunk := rest
		if i >= 0 {
			chunk = rest[:i]
		}
		if !w.skip {
			room := maxLine - len(w.pending)
			if len(chunk) > room {
				chunk, w.skip = chunk[:room], true
			}
			w.pending = append(w.pending, chunk...)
		}
		if i < 0 {
			break
		}
		w.emit()
		rest = rest[i+1:]
	}
	return len(p), nil
}

func (w *lineWriter) emit() {
	line := strings.TrimSuffix(string(w.pending), "\r")
	w.pending, w.skip = w.pending[:0], false
	w.sink.add(line)
}

func (w *lineWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.pending) > 0 {
		w.emit()
	}
}

func wrapRunError(bin string, args []string, stdout, stderr string, err error) error {
	msg := stderr
	if msg == "" {
		msg = stdout
	}
	if msg == "" {
		msg = err.Error()
	}
	return &RunError{
		Binary: bin,
		Args:   append([]string(nil), redact(args)...),
		Err:    err,
		Output: msg,
	}
}

func redact(args []string) []string {
	out := append([]string(nil), args...)
	for i := range out {
		switch {
		case out[i] == "--secret" && i+1 < len(out):
			out[i+1] = "<redacted>"
		case strings.HasPrefix(out[i], "--secret="):
			out[i] = "--secret=<redacted>"
		}
	}
	return out
}

// RunError is a failed pangolin invocation.
type RunError struct {
	Binary string
	Args   []string
	Err    error
	Output string
}

func (e *RunError) Error() string {
	if e.Output != "" {
		return fmt.Sprintf("pangolin %s: %s", strings.Join(e.Args, " "), lastLines(e.Output, 3))
	}
	return fmt.Sprintf("pangolin %s: %v", strings.Join(e.Args, " "), e.Err)
}

func (e *RunError) Unwrap() error { return e.Err }

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " ")
}

// ConfigDir is the CLI's per-user config directory. The CLI always uses
// ~/.config/pangolin and does not follow XDG_CONFIG_HOME, so neither does
// this: anywhere else would not be where the CLI keeps its files.
func ConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		u, uerr := user.Current()
		if uerr != nil || u.HomeDir == "" {
			return filepath.Join(".", ".config", "pangolin")
		}
		home = u.HomeDir
	}
	return filepath.Join(home, ".config", "pangolin")
}

func accountsPath() string { return filepath.Join(ConfigDir(), "accounts.json") }
func configPath() string   { return filepath.Join(ConfigDir(), "config.json") }

// DefaultLogPath is where the CLI writes the tunnel log by default.
func DefaultLogPath() string { return filepath.Join(ConfigDir(), "logs", "client.log") }
