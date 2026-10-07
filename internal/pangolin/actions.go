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
	"errors"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/lucasssvaz/traygolin/internal/privhelper"
)

// ErrHelperMissing means the polkit helper is not installed, so the CLI
// cannot start its root tunnel process without a terminal sudo prompt.
var ErrHelperMissing = errors.New("the Traygolin privileged helper is not installed")

// ErrNotAuthorized means polkit refused or the user dismissed the prompt.
var ErrNotAuthorized = errors.New("authorization to change the VPN connection was denied")

// ErrDeviceSetup means the saved device credentials are no longer valid
// and the CLI needs to run once as root before it can register this
// device again. Signing in again keeps the old credentials, so it does
// not help.
var ErrDeviceSetup = errors.New("pangolin needs a one-time administrator setup to register this device")

// UpOptions are Traygolin-specific flags for `pangolin up`. DNS and
// routing options come from the CLI's own config.json.
type UpOptions struct {
	MTU          int
	Holepunch    bool
	DisableRelay bool
}

// UpArgs returns the CLI arguments for opts.
func UpArgs(opts UpOptions) []string {
	args := []string{"up", "--silent"}
	if opts.MTU > 0 && opts.MTU != 1280 {
		args = append(args, "--mtu", strconv.Itoa(opts.MTU))
	}
	if !opts.Holepunch {
		args = append(args, "--holepunch=false")
	}
	if opts.DisableRelay {
		args = append(args, "--disable-relay")
	}
	return args
}

// Up starts the tunnel. The CLI does its usual work as this user and
// elevates through the Traygolin sudo shim, which uses polkit.
func (c *Client) Up(ctx context.Context, opts UpOptions) error {
	if !privhelper.Ready() {
		return ErrHelperMissing
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	dir, err := privhelper.ShimDir(self)
	if err != nil {
		return err
	}
	env := []string{"PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH")}
	_, err = c.output(ctx, 2*time.Minute, env, UpArgs(opts)...)
	return classifyHelperError(err)
}

func classifyHelperError(err error) error {
	if err == nil {
		return nil
	}
	low := strings.ToLower(err.Error())
	switch {
	case strings.Contains(low, "privileged helper is not installed"):
		return ErrHelperMissing
	case strings.Contains(low, "authorization was denied"), strings.Contains(low, "not authorized"):
		return ErrNotAuthorized
	case strings.Contains(low, "rerun this command as sudo"):
		return ErrDeviceSetup
	}
	return err
}

// ResetDNS restores the system DNS through the helper.
func (c *Client) ResetDNS(ctx context.Context) error {
	if !privhelper.Ready() {
		return ErrHelperMissing
	}
	return classifyHelperError(privhelper.ResetDNS(ctx))
}

var newVersionRe = regexp.MustCompile(`new version is available: v?(\S+)`)

// LatestVersion returns the newer CLI release that `pangolin version`
// reports, or "" if it reports none. It never installs anything: the
// CLI's own update needs root and runs a downloaded script.
func (c *Client) LatestVersion(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := c.command(ctx, nil, "version").CombinedOutput()
	if err != nil {
		return "", wrapRunError(c.bin(), []string{"version"}, string(out), "", err)
	}
	if m := newVersionRe.FindStringSubmatch(string(out)); m != nil {
		return m[1], nil
	}
	return "", nil
}

func (c *Client) ListAliases(ctx context.Context) (string, error) {
	return c.output(ctx, 8*time.Second, nil, "list", "aliases")
}
