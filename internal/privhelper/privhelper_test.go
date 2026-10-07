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
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// cliScript builds the detached-mode shell command in the shape the
// Pangolin CLI 0.18 produces: Go %q-quoted words after nohup.
func cliScript(keyring bool, words ...string) string {
	s := "export PANGOLIN_SUBPROCESS=1 && "
	if keyring {
		s += "export PANGOLIN_CREDENTIALS_FROM_KEYRING=1 && "
	}
	s += "nohup"
	for _, w := range words {
		s += " " + fmt.Sprintf("%q", w)
	}
	return s + " >/dev/null 2>&1 &"
}

var upWords = []string{
	"/usr/local/bin/pangolin", "up", "client",
	"--org", "org_123",
	"--id", "olm-abc",
	"--secret", `s3cr"et\with spaces`,
	"--endpoint", "https://app.pangolin.net",
	"--mtu", "1400",
	"--holepunch=false",
	"--match-domains", "*.proxy.internal,corp.example",
	"--prefer-local-routes",
	"--exit-node-site-ids", "1,3",
	"--exit-node-resource-id", "9",
}

func TestParseSudoKeyring(t *testing.T) {
	l, err := ParseSudo([]string{"sh", "-c", cliScript(true, upWords...)})
	if err != nil {
		t.Fatal(err)
	}
	if !l.Keyring || l.Exe != "/usr/local/bin/pangolin" {
		t.Fatalf("%+v", l)
	}
	if !slices.Equal(l.Args, upWords[1:]) {
		t.Fatalf("args\n got %q\nwant %q", l.Args, upWords[1:])
	}
	if err := ValidateUpArgs(l.Args); err != nil {
		t.Fatal(err)
	}
}

func TestParseSudoNoKeyring(t *testing.T) {
	l, err := ParseSudo([]string{"sh", "-c", cliScript(false, "/usr/bin/pangolin", "up", "client", "--id", "x", "--secret", "y", "--endpoint", "https://p.example")})
	if err != nil {
		t.Fatal(err)
	}
	if l.Keyring {
		t.Fatal("keyring should be false")
	}
}

func TestParseSudoRejects(t *testing.T) {
	cases := [][]string{
		{"-v"},
		{"sh", "-c"},
		{"bash", "-c", cliScript(true, upWords...)},
		{"sh", "-c", "rm -rf /"},
		{"sh", "-c", "export PANGOLIN_SUBPROCESS=1 && nohup \"/bin/sh\" \"-c\" \"id\""},
		{"sh", "-c", "export PANGOLIN_SUBPROCESS=1 && nohup /bin/sh -c id >/dev/null 2>&1 &"},
		{"sh", "-c", "export PANGOLIN_SUBPROCESS=1 && nohup \"a\"\"b\" \"c\" >/dev/null 2>&1 &"},
		{"sh", "-c", "export PANGOLIN_SUBPROCESS=1 && nohup \"unterminated >/dev/null 2>&1 &"},
		{"sh", "-c", cliScript(true, upWords...) + "; id"},
	}
	for _, args := range cases {
		if _, err := ParseSudo(args); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%q: want ErrUnsupported, got %v", args, err)
		}
	}
}

func TestValidateUpArgsRejects(t *testing.T) {
	base := []string{"up", "client", "--id", "a", "--secret", "b", "--endpoint", "https://p.example"}
	bad := [][]string{
		{"down"},
		{"up", "site", "--id", "a"},
		append(slices.Clone(base), "--tls-client-cert", "/etc/shadow"),
		append(slices.Clone(base), "--http-addr", "0.0.0.0:80"),
		append(slices.Clone(base), "--attach"),
		append(slices.Clone(base), "--subnet-router"),
		append(slices.Clone(base), "positional"),
		append(slices.Clone(base), "--mtu", "99999"),
		append(slices.Clone(base), "--log-level", "trace"),
		append(slices.Clone(base), "--interface-name", "eth0;rm"),
		append(slices.Clone(base), "--holepunch=maybe"),
		append(slices.Clone(base), "--id", "dup"),
		append(slices.Clone(base), "--org"),
		{"up", "client", "--id", "a", "--secret", "b", "--endpoint", "file:///etc/passwd"},
		{"up", "client", "--secret", "b", "--endpoint", "https://p.example"},
	}
	for _, args := range bad {
		if err := ValidateUpArgs(args); err == nil {
			t.Errorf("%q: should be rejected", args)
		}
	}
	good := append(slices.Clone(base), "--override-dns", "--tunnel-dns=false", "--upstream-dns", "1.1.1.1:53,[2606:4700::1111]:53", "--ping-interval", "5s", "--log-level=debug")
	if err := ValidateUpArgs(good); err != nil {
		t.Fatal(err)
	}
}

func TestTrustedBinary(t *testing.T) {
	uid := os.Getuid()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	good := filepath.Join(dir, "pangolin")
	if err := os.WriteFile(good, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	writable := filepath.Join(dir, "pangolin-w")
	if err := os.WriteFile(writable, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(writable, 0o777); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(good, link); err != nil {
		t.Fatal(err)
	}

	if got, err := TrustedBinary([]string{filepath.Join(dir, "missing"), writable, link}, uid); err != nil || got != good {
		t.Fatalf("got %q %v", got, err)
	}
	if _, err := TrustedBinary([]string{writable}, uid); err == nil {
		t.Fatal("world-writable binary accepted")
	}
	if _, err := TrustedBinary([]string{good}, uid+1); err == nil {
		t.Fatal("wrong owner accepted")
	}

	open := filepath.Join(t.TempDir(), "open")
	if err := os.Mkdir(open, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(open, 0o777); err != nil {
		t.Fatal(err)
	}
	inOpen := filepath.Join(open, "pangolin")
	if err := os.WriteFile(inOpen, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := TrustedBinary([]string{inOpen}, uid); err == nil {
		t.Fatal("binary in world-writable dir accepted")
	}
}

func TestUpEnvUsesCaller(t *testing.T) {
	u := &user.User{Username: "alice", Uid: "1000", Gid: "1000"}
	env := upEnv(u, true)
	for _, want := range []string{"SUDO_USER=alice", "SUDO_UID=1000", "SUDO_GID=1000", "PANGOLIN_SUBPROCESS=1", "PANGOLIN_CREDENTIALS_FROM_KEYRING=1", "HOME=/root"} {
		if !slices.Contains(env, want) {
			t.Errorf("missing %s in %q", want, env)
		}
	}
	if slices.Contains(upEnv(u, false), "PANGOLIN_CREDENTIALS_FROM_KEYRING=1") {
		t.Error("keyring env without keyring")
	}
}

func TestMoveCredentials(t *testing.T) {
	args := []string{"up", "client", "--org", "o", "--id", "olm-abc", "--secret=s3cr et", "--endpoint", "https://x", "--holepunch=false"}
	rest, env := moveCredentials(args)
	if want := []string{"up", "client", "--org", "o", "--endpoint", "https://x", "--holepunch=false"}; !slices.Equal(rest, want) {
		t.Errorf("args %q", rest)
	}
	if want := []string{"PANGOLIN_CLIENT_ID=olm-abc", "PANGOLIN_CLIENT_SECRET=s3cr et"}; !slices.Equal(env, want) {
		t.Errorf("env %q", env)
	}
	if err := ValidateUpArgs(append(slices.Clone(rest), "--id", "a", "--secret", "b")); err != nil {
		t.Errorf("remaining args no longer validate: %v", err)
	}
}

func TestReadsCredentialEnv(t *testing.T) {
	dir := t.TempDir()
	newCLI := filepath.Join(dir, "new")
	oldCLI := filepath.Join(dir, "old")
	if err := os.WriteFile(newCLI, []byte("\x7fELF...PANGOLIN_CLIENT_SECRET..."), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldCLI, []byte("\x7fELF...PANGOLIN_SUBPROCESS..."), 0o755); err != nil {
		t.Fatal(err)
	}
	if !readsCredentialEnv(newCLI) || readsCredentialEnv(oldCLI) || readsCredentialEnv(filepath.Join(dir, "missing")) {
		t.Fatal("credential env detection")
	}
}

func listen(t *testing.T, path string) net.Listener {
	t.Helper()
	_ = os.Remove(path)
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}

func TestRestrictSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "olm.sock")
	stale := listen(t, path)
	old := socketID(path)
	stale.Close()

	go func() {
		time.Sleep(100 * time.Millisecond)
		listen(t, path)
	}()
	if err := restrictSocket(path, old, os.Getuid(), 5*time.Second); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if socketID(path) == old || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v, replaced %v", fi.Mode().Perm(), socketID(path) != old)
	}
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal("owner can no longer connect:", err)
	}
	conn.Close()

	if err := restrictSocket(filepath.Join(t.TempDir(), "never.sock"), fileID{}, os.Getuid(), 100*time.Millisecond); err != nil {
		t.Fatal("a socket that never appears is not an error:", err)
	}
}

func TestRestrictSocketReusedInode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "olm.sock")
	listen(t, path)
	old := socketID(path)
	old.ctime.Sec--
	if err := restrictSocket(path, old, os.Getuid(), time.Second); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("a new socket with a reused inode number was not restricted: %v", fi.Mode().Perm())
	}
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatal(err)
	}
	same := socketID(path)
	if err := restrictSocket(path, same, os.Getuid(), 100*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(path); fi.Mode().Perm() != 0o666 {
		t.Fatalf("the unchanged old socket was touched: %v", fi.Mode().Perm())
	}
}

func TestTunnelCommand(t *testing.T) {
	run := filepath.Join(t.TempDir(), "systemd-run")
	if err := os.WriteFile(run, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	args := []string{"up", "client", "--org", "o"}
	direct := []string{"/usr/bin/pangolin", "up", "client", "--org", "o"}

	got := tunnelCommand("/usr/bin/pangolin", args, true, []string{"/missing/systemd-run", run})
	want := append([]string{run, "--scope", "--quiet", "--collect", "--description=Traygolin tunnel", "--"}, direct...)
	if !slices.Equal(got, want) {
		t.Errorf("scoped: %q", got)
	}
	if got := tunnelCommand("/usr/bin/pangolin", args, false, []string{run}); !slices.Equal(got, direct) {
		t.Errorf("not booted with systemd: %q", got)
	}
	if got := tunnelCommand("/usr/bin/pangolin", args, true, []string{"/missing/systemd-run"}); !slices.Equal(got, direct) {
		t.Errorf("no systemd-run: %q", got)
	}
}

func TestHelperMainRequiresRoot(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root")
	}
	var errb bytes.Buffer
	if code := HelperMain([]string{"up"}, strings.NewReader("{}"), &errb); code == 0 {
		t.Fatal("non-root helper should fail")
	}
}

func TestRunShimRejectsUnknown(t *testing.T) {
	var errb bytes.Buffer
	if code := RunShim([]string{"-k"}, &errb); code == 0 {
		t.Fatal("should fail")
	}
	if !strings.Contains(errb.String(), "unsupported") {
		t.Fatalf("%q", errb.String())
	}
}

func TestShimDir(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	self := "/usr/bin/traygolin"
	dir, err := ShimDir(self)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.Readlink(filepath.Join(dir, "sudo"))
	if err != nil || got != self {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := ShimDir("/opt/other"); err != nil {
		t.Fatal(err)
	}
	got, _ = os.Readlink(filepath.Join(dir, "sudo"))
	if got != "/opt/other" {
		t.Fatalf("not replaced: %q", got)
	}
}

func TestSetupScriptSkipsMissingAndSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real")
	if err := os.WriteFile(target, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "pangolin")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("/bin/sh", "-c", setupScript, "sh", "false", "", "", "", "", filepath.Join(dir, "missing"), link).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if info, _ := os.Stat(target); info.Mode().Perm() != 0o755 {
		t.Fatalf("target changed: %v", info.Mode())
	}
}

func TestRenderPolicy(t *testing.T) {
	out := string(RenderPolicy([]byte(`<annotate key="org.freedesktop.policykit.exec.path">@HELPER@</annotate>`)))
	if !strings.Contains(out, HelperPath) || strings.Contains(out, "@HELPER@") {
		t.Fatal(out)
	}
}
