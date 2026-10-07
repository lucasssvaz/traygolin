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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lucasssvaz/traygolin/internal/olm"
)

// HelperPath is where the root helper is installed. The polkit policy
// pins this exact path. It can be overridden at build time with -X.
var HelperPath = "/usr/lib/traygolin/traygolin-helper"

// PolicyPath is where the polkit action is installed.
const PolicyPath = "/usr/share/polkit-1/actions/io.github.lucasssvaz.Traygolin.policy"

// ActionID is the polkit action that authorizes the helper.
const ActionID = "io.github.lucasssvaz.Traygolin.tunnel"

// TrustedPangolin lists the only binaries the helper will run as root.
var TrustedPangolin = []string{"/usr/bin/pangolin", "/usr/local/bin/pangolin"}

// UpRequest is sent to the helper on stdin so secrets stay out of argv
// of the pkexec process.
type UpRequest struct {
	Keyring bool     `json:"keyring"`
	Args    []string `json:"args"`
}

// Installed reports whether the helper and its polkit policy are present.
func Installed() bool {
	for _, p := range []string{HelperPath, PolicyPath} {
		if _, err := os.Stat(p); err != nil {
			return false
		}
	}
	return true
}

// Ready reports whether passwordless connect can work: the helper is
// installed and there is a pangolin binary it is willing to run.
func Ready() bool {
	if !Installed() {
		return false
	}
	_, err := TrustedBinary(TrustedPangolin, 0)
	return err == nil
}

// TrustedBinary returns the first candidate that resolves to a regular
// file owned by ownerUID, in a directory owned by ownerUID, and that
// neither is group- or world-writable.
func TrustedBinary(candidates []string, ownerUID int) (string, error) {
	var errs []error
	for _, c := range candidates {
		real, err := filepath.EvalSymlinks(c)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if err := checkOwned(real, ownerUID, true); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := checkOwned(filepath.Dir(real), ownerUID, false); err != nil {
			errs = append(errs, err)
			continue
		}
		return real, nil
	}
	if len(errs) == 0 {
		return "", errors.New("no pangolin binary candidates")
	}
	return "", fmt.Errorf("no trusted pangolin binary: %w", errors.Join(errs...))
}

func checkOwned(path string, ownerUID int, regular bool) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if regular && !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%s is group- or world-writable", path)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != ownerUID {
		return fmt.Errorf("%s is not owned by uid %d", path, ownerUID)
	}
	return nil
}

// HelperMain is the entry point of traygolin-helper, run as root by pkexec.
func HelperMain(args []string, stdin io.Reader, stderr io.Writer) int {
	if os.Geteuid() != 0 {
		fmt.Fprintln(stderr, "traygolin-helper must be run through pkexec")
		return 1
	}
	caller, err := pkexecCaller()
	if err != nil {
		fmt.Fprintln(stderr, "traygolin-helper:", err)
		return 1
	}
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: traygolin-helper up|down")
		return 2
	}
	switch args[0] {
	case "up":
		var req UpRequest
		if err := json.NewDecoder(io.LimitReader(stdin, 64*1024)).Decode(&req); err != nil {
			fmt.Fprintln(stderr, "traygolin-helper: read request:", err)
			return 2
		}
		if err := ValidateUpArgs(req.Args); err != nil {
			fmt.Fprintln(stderr, "traygolin-helper:", err)
			return 2
		}
		bin, err := TrustedBinary(TrustedPangolin, 0)
		if err != nil {
			fmt.Fprintln(stderr, "traygolin-helper:", err)
			return 1
		}
		args, env := req.Args, upEnv(caller, req.Keyring)
		if readsCredentialEnv(bin) {
			var creds []string
			args, creds = moveCredentials(args)
			env = append(env, creds...)
		}
		old := socketInode(olm.DefaultSocket)
		if err := spawnDetached(bin, args, env); err != nil {
			fmt.Fprintln(stderr, "traygolin-helper: start pangolin:", err)
			return 1
		}
		if uid, err := strconv.Atoi(caller.Uid); err == nil {
			if err := restrictSocket(olm.DefaultSocket, old, uid, 10*time.Second); err != nil {
				fmt.Fprintln(stderr, "traygolin-helper: restrict olm socket:", err)
			}
		}
		return 0
	case "down":
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		c := olm.New("")
		if err := c.Exit(ctx); err != nil && !errors.Is(err, olm.ErrNotRunning) {
			fmt.Fprintln(stderr, "traygolin-helper:", err)
			return 1
		}
		_ = c.WaitStopped(ctx)
		return 0
	default:
		fmt.Fprintln(stderr, "traygolin-helper: unknown command", args[0])
		return 2
	}
}

func pkexecCaller() (*user.User, error) {
	raw := os.Getenv("PKEXEC_UID")
	if raw == "" {
		return nil, errors.New("PKEXEC_UID is not set; run through pkexec")
	}
	if _, err := strconv.Atoi(raw); err != nil {
		return nil, fmt.Errorf("invalid PKEXEC_UID %q", raw)
	}
	return user.LookupId(raw)
}

// upEnv mirrors what sudo gives the CLI's root subprocess, so it reads
// the caller's ~/.config/pangolin rather than root's.
func upEnv(u *user.User, keyring bool) []string {
	env := []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/root",
		"USER=root",
		"LOGNAME=root",
		"SUDO_USER=" + u.Username,
		"SUDO_UID=" + u.Uid,
		"SUDO_GID=" + u.Gid,
		envSubprocess + "=1",
	}
	if keyring {
		env = append(env, envKeyring+"=1")
	}
	if lang := os.Getenv("LANG"); lang != "" && !strings.ContainsAny(lang, "\n\x00") {
		env = append(env, "LANG="+lang)
	}
	return env
}

// credentialEnv maps the credential flags to the variables the CLI reads
// (since 0.17.0) when the flags are absent.
var credentialEnv = map[string]string{
	"id":     "PANGOLIN_CLIENT_ID",
	"secret": "PANGOLIN_CLIENT_SECRET",
}

// readsCredentialEnv reports whether the CLI binary supports credentialEnv.
// Running `pangolin version` would also check for updates online.
func readsCredentialEnv(bin string) bool {
	data, err := os.ReadFile(bin)
	return err == nil && bytes.Contains(data, []byte(credentialEnv["secret"]))
}

// moveCredentials takes --id and --secret out of validated `up client`
// args and returns them as environment entries instead. Arguments stay
// world-readable in /proc for the life of the tunnel; the environment of a
// root process is readable only by root.
func moveCredentials(args []string) (rest, env []string) {
	rest = make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		name, value, hasValue := strings.Cut(strings.TrimPrefix(args[i], "--"), "=")
		key, ok := credentialEnv[name]
		if !ok || !strings.HasPrefix(args[i], "--") {
			rest = append(rest, args[i])
			continue
		}
		if !hasValue && i+1 < len(args) {
			i++
			value = args[i]
		}
		env = append(env, key+"="+value)
	}
	return rest, env
}

func socketInode(path string) uint64 {
	var st syscall.Stat_t
	if err := syscall.Lstat(path, &st); err != nil {
		return 0
	}
	return st.Ino
}

// restrictSocket waits for olm to replace the socket at path (old is the
// inode that was there before, or 0) and limits it to uid. olm makes its
// API socket world-writable, which lets any local user disconnect the
// tunnel or switch its organization and exit node.
func restrictSocket(path string, old uint64, uid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		var st syscall.Stat_t
		if err := syscall.Lstat(path, &st); err == nil && st.Mode&syscall.S_IFMT == syscall.S_IFSOCK && st.Ino != old {
			if err := os.Lchown(path, uid, -1); err != nil {
				return err
			}
			return os.Chmod(path, 0o600)
		}
		if time.Now().After(deadline) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// systemdRun is used to give the tunnel its own scope. Without it the
// tunnel inherits the caller's cgroup, so it would keep the app's
// autostart unit or terminal scope alive, and logout would stall trying
// to stop root processes it cannot kill.
var systemdRun = []string{"/usr/bin/systemd-run", "/bin/systemd-run"}

// tunnelCommand returns the argv that starts pangolin, wrapped in a
// transient system scope when systemd is the init system.
func tunnelCommand(bin string, args []string, booted bool, runPaths []string) []string {
	if booted {
		for _, run := range runPaths {
			if fi, err := os.Stat(run); err == nil && fi.Mode().IsRegular() {
				scope := []string{run, "--scope", "--quiet", "--collect", "--description=Traygolin tunnel", "--"}
				return append(append(scope, bin), args...)
			}
		}
	}
	return append([]string{bin}, args...)
}

func systemdBooted() bool {
	fi, err := os.Stat("/run/systemd/system")
	return err == nil && fi.IsDir()
}

func spawnDetached(bin string, args, env []string) error {
	devnull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer devnull.Close()
	argv := tunnelCommand(bin, args, systemdBooted(), systemdRun)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = env
	cmd.Dir = "/"
	cmd.Stdin = devnull
	cmd.Stdout = devnull
	cmd.Stderr = devnull
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// RunShim is the entry point when traygolin runs as "sudo" on PATH.
func RunShim(args []string, stderr io.Writer) int {
	l, err := ParseSudo(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if !Installed() {
		fmt.Fprintln(stderr, "traygolin: the privileged helper is not installed. Open Traygolin and click Set up, or install the traygolin package.")
		return 1
	}
	pkexec := "/usr/bin/pkexec"
	if _, err := os.Stat(pkexec); err != nil {
		fmt.Fprintln(stderr, "traygolin: pkexec (polkit) is required")
		return 1
	}
	req, err := json.Marshal(UpRequest{Keyring: l.Keyring, Args: l.Args})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	cmd := exec.Command(pkexec, HelperPath, "up")
	cmd.Stdin = strings.NewReader(string(req))
	cmd.Stdout = stderr
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			if code := exit.ExitCode(); code == 126 || code == 127 {
				fmt.Fprintln(stderr, "traygolin: authorization was denied or dismissed")
			}
			return exit.ExitCode()
		}
		fmt.Fprintln(stderr, "traygolin:", err)
		return 1
	}
	return 0
}

// PrivilegedDown asks the helper to stop the tunnel as root. It is only
// needed when the user cannot reach the tunnel socket directly.
func PrivilegedDown(ctx context.Context) error {
	if !Installed() {
		return errors.New("the privileged helper is not installed")
	}
	out, err := exec.CommandContext(ctx, "/usr/bin/pkexec", HelperPath, "down").CombinedOutput()
	if err != nil {
		return fmt.Errorf("traygolin-helper down: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ShimDir creates a private directory containing a "sudo" symlink to
// self and returns it, for prepending to the CLI's PATH.
func ShimDir(self string) (string, error) {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		base = filepath.Join(os.TempDir(), "traygolin-"+strconv.Itoa(os.Getuid()))
	}
	dir := filepath.Join(base, "traygolin", "bin")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := checkOwned(dir, os.Getuid(), false); err != nil {
		return "", err
	}
	link := filepath.Join(dir, "sudo")
	if cur, err := os.Readlink(link); err == nil && cur == self {
		return dir, nil
	}
	_ = os.Remove(link)
	if err := os.Symlink(self, link); err != nil {
		return "", err
	}
	return dir, nil
}

// Setup installs the helper binary and polkit policy with one admin
// prompt, and hands user-owned pangolin binaries in the trusted
// locations to root so the helper will run them. If helperSrc is empty,
// only the binaries are fixed.
func Setup(ctx context.Context, helperSrc string, policy []byte) error {
	install := helperSrc != ""
	if install {
		if _, err := os.Stat(helperSrc); err != nil {
			return fmt.Errorf("helper binary not found next to traygolin: %w", err)
		}
	}
	tmp, err := os.CreateTemp("", "traygolin-policy-*.xml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(RenderPolicy(policy)); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	args := append([]string{"/bin/sh", "-c", setupScript, "sh", strconv.FormatBool(install), helperSrc, tmp.Name(), HelperPath, PolicyPath}, TrustedPangolin...)
	out, err := exec.CommandContext(ctx, "/usr/bin/pkexec", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("set up: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// setupScript runs as root. Symlinked binaries are left alone: their
// target may live in a user-owned directory, which TrustedBinary rejects
// anyway.
const setupScript = `set -e
if [ "$1" = true ]; then
	install -D -m 0755 -o root -g root "$2" "$4"
	install -D -m 0644 -o root -g root "$3" "$5"
fi
shift 5
for p in "$@"; do
	if [ -f "$p" ] && [ ! -L "$p" ]; then
		chown root:root "$p"
		chmod go-w "$p"
	fi
done`

// RenderPolicy fills the helper path into the polkit policy template.
func RenderPolicy(tmpl []byte) []byte {
	return []byte(strings.ReplaceAll(string(tmpl), "@HELPER@", HelperPath))
}

// HelperBesides returns the expected helper path next to exe.
func HelperBesides(exe string) string {
	return filepath.Join(filepath.Dir(exe), "traygolin-helper")
}
