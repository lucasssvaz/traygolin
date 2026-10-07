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
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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

func (c *Client) command(ctx context.Context, extraEnv []string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, c.bin(), args...)
	cmd.Env = append(append(os.Environ(), c.Env...), extraEnv...)
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
	err := cmd.Run()
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
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", wrapRunError(c.bin(), args, "", "", err)
	}
	var (
		mu  sync.Mutex
		all strings.Builder
		wg  sync.WaitGroup
	)
	scan := func(r io.Reader) {
		defer wg.Done()
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			mu.Lock()
			all.WriteString(line)
			all.WriteByte('\n')
			mu.Unlock()
			if onLine != nil {
				onLine(line)
			}
		}
	}
	wg.Add(2)
	go scan(stdout)
	go scan(stderr)
	wg.Wait()
	waitErr := cmd.Wait()
	out := strings.TrimSpace(all.String())
	if waitErr != nil {
		return out, wrapRunError(c.bin(), args, out, "", waitErr)
	}
	return out, nil
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
		if out[i] == "--secret" && i+1 < len(out) {
			out[i+1] = "<redacted>"
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

// ConfigDir is the CLI's per-user config directory.
func ConfigDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "pangolin")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".config", "pangolin")
	}
	return filepath.Join(home, ".config", "pangolin")
}

func accountsPath() string { return filepath.Join(ConfigDir(), "accounts.json") }
func configPath() string   { return filepath.Join(ConfigDir(), "config.json") }

// DefaultLogPath is where the CLI writes the tunnel log by default.
func DefaultLogPath() string { return filepath.Join(ConfigDir(), "logs", "client.log") }
