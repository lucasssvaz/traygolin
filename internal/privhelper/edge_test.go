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
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

// base is the smallest argument list that passes validation.
func base(extra ...string) []string {
	return append([]string{"up", "client", "--id", "a", "--secret", "b", "--endpoint", "https://p.example"}, extra...)
}

func TestValidateUpArgsTable(t *testing.T) {
	ok := map[string][]string{
		"org":             base("--org", "org_1"),
		"org with equals": base("--org=org_1"),
		"ident charset":   base("--org", "Az09._:@-"),
		"max ident":       base("--org", strings.Repeat("a", 256)),
		"mtu low":         base("--mtu", "576"),
		"mtu high":        base("--mtu=9000"),
		"netstack dns":    base("--netstack-dns", "[::1]:53"),
		"iface":           base("--interface-name", "pangolin0"),
		"iface max":       base("--interface-name", strings.Repeat("a", 15)),
		"ping 1h":         base("--ping-interval", "1h"),
		"ping fraction":   base("--ping-timeout", "1.5s"),
		"upstream list":   base("--upstream-dns", "1.1.1.1,8.8.8.8:53"),
		"match domains":   base("--match-domains", "*.a.example,b_c.example,x?.example"),
		"32 site ids":     base("--exit-node-site-ids", strings.TrimSuffix(strings.Repeat("1,", 32), ",")),
		// The CLI forwards match domains from config.json unchecked and
		// without a limit, so long lists and IDN names must not stop a
		// connection.
		"256 match domains":       base("--match-domains", strings.TrimSuffix(strings.Repeat("*.a.example,", 256), ",")),
		"256 upstream":            base("--upstream-dns", strings.TrimSuffix(strings.Repeat("1.1.1.1,", 256), ",")),
		"idn match domain":        base("--match-domains", "*.bücher.example,münchen.example,例え.テスト"),
		"resource id max":         base("--exit-node-resource-id", "2147483647"),
		"bool bare":               base("--holepunch", "--override-dns", "--tunnel-dns"),
		"bool forms":              base("--holepunch=t", "--tunnel-dns=FALSE", "--override-dns=0"),
		"all bools":               base("--prefer-local-routes", "--exit-node-takes-precedence", "--disable-relay"),
		"http endpoint":           append(base()[:6:6], "--endpoint", "http://10.0.0.1:3000"),
		"mixed case scheme":       append(base()[:6:6], "--endpoint", "HTTPS://P.EXAMPLE"),
		"secret with symbols":     append(base()[:4:4], "--secret", `p@ss w"rd'$(x);|&`, "--endpoint", "https://p.example"),
		"secret at 512":           append(base()[:4:4], "--secret", strings.Repeat("s", 512), "--endpoint", "https://p.example"),
		"secret utf-8":            append(base()[:4:4], "--secret", "pässwörd-日本", "--endpoint", "https://p.example"),
		"secret starts with dash": append(base()[:4:4], "--secret", "-leading", "--endpoint", "https://p.example"),
	}
	for name, args := range ok {
		if err := ValidateUpArgs(args); err != nil {
			t.Errorf("%s: rejected: %v\n%q", name, err, args)
		}
	}

	bad := map[string][]string{
		"nil":                nil,
		"just up":            {"up"},
		"wrong verb":         {"upp", "client"},
		"wrong target":       {"up", "Client"},
		"extra before":       {"x", "up", "client", "--id", "a", "--secret", "b", "--endpoint", "https://p"},
		"missing id":         {"up", "client", "--secret", "b", "--endpoint", "https://p.example"},
		"missing endpoint":   {"up", "client", "--id", "a", "--secret", "b"},
		"empty id":           base("--id="),
		"short flag":         base("-o", "x"),
		"single dash long":   base("-org", "x"),
		"upper flag":         base("--ORG", "x"),
		"bare double dash":   base("--"),
		"double dash value":  base("--=x"),
		"unknown flag":       base("--config", "/etc/x"),
		"unknown with value": base("--attach=true"),
		"empty arg":          base(""),
		"space arg":          base(" "),
		"org empty":          base("--org", ""),
		"org too long":       base("--org", strings.Repeat("a", 257)),
		"org slash":          base("--org", "a/b"),
		"org space":          base("--org", "a b"),
		"org newline":        base("--org", "a\nb"),
		"org trailing nl":    base("--org", "ok\n"),
		"org unicode":        base("--org", "ö"),
		"org nul":            base("--org", "a\x00b"),
		"mtu low":            base("--mtu", "575"),
		"mtu high":           base("--mtu", "9001"),
		"mtu text":           base("--mtu", "big"),
		"mtu float":          base("--mtu", "1400.5"),
		"mtu hex":            base("--mtu", "0x578"),
		"mtu empty":          base("--mtu="),
		"mtu overflow":       base("--mtu", "99999999999999999999999"),
		"iface too long":     base("--interface-name", strings.Repeat("a", 16)),
		"iface slash":        base("--interface-name", "a/b"),
		"iface dot":          base("--interface-name", "a.b"),
		"log level":          base("--log-level", "DEBUG"),
		"ping zero":          base("--ping-interval", "0s"),
		"ping negative":      base("--ping-interval", "-5s"),
		"ping too long":      base("--ping-timeout", "1h1s"),
		"ping bare number":   base("--ping-interval", "5"),
		"ping text":          base("--ping-interval", "soon"),
		"upstream empty":     base("--upstream-dns", ""),
		"upstream trailing":  base("--upstream-dns", "1.1.1.1,"),
		"upstream leading":   base("--upstream-dns", ",1.1.1.1"),
		"upstream bad":       base("--upstream-dns", "1.1.1.1,a b"),
		"upstream 257":       base("--upstream-dns", strings.TrimSuffix(strings.Repeat("1.1.1.1,", 257), ",")),
		"match 257":          base("--match-domains", strings.TrimSuffix(strings.Repeat("*.a.example,", 257), ",")),
		"match empty item":   base("--match-domains", "a.example,,b.example"),
		"match symbol":       base("--match-domains", "a.example;b.example"),
		"match bad":          base("--match-domains", "a.example,$(id)"),
		"site id sign":       base("--exit-node-site-ids", "1,-2"),
		"site id big":        base("--exit-node-site-ids", "12345678901"),
		"resource zero":      base("--exit-node-resource-id", "0"),
		"resource negative":  base("--exit-node-resource-id", "-1"),
		"resource too big":   base("--exit-node-resource-id", "2147483648"),
		"bool junk":          base("--holepunch=yes"),
		"bool empty":         base("--holepunch="),
		"bool separate":      base("--holepunch", "false"),
		"duplicate bool":     base("--holepunch", "--holepunch=false"),
		"duplicate mixed":    base("--org", "a", "--org=b"),
		"duplicate id":       base("--id=z"),
		"endpoint no scheme": append(base()[:6:6], "--endpoint", "p.example"),
		"endpoint ftp":       append(base()[:6:6], "--endpoint", "ftp://p.example"),
		"endpoint file":      append(base()[:6:6], "--endpoint", "file:///etc/passwd"),
		"endpoint no host":   append(base()[:6:6], "--endpoint", "https://"),
		"endpoint js":        append(base()[:6:6], "--endpoint", "javascript:alert(1)"),
		"endpoint newline":   append(base()[:6:6], "--endpoint", "https://p.example\nx"),
		"endpoint space":     append(base()[:6:6], "--endpoint", "https://p .example"),
		"secret empty":       append(base()[:4:4], "--secret", "", "--endpoint", "https://p.example"),
		"secret too long":    append(base()[:4:4], "--secret", strings.Repeat("s", 513), "--endpoint", "https://p.example"),
		"secret newline":     append(base()[:4:4], "--secret", "a\nb", "--endpoint", "https://p.example"),
		"secret cr":          append(base()[:4:4], "--secret", "a\rb", "--endpoint", "https://p.example"),
		"secret nul":         append(base()[:4:4], "--secret", "a\x00b", "--endpoint", "https://p.example"),
		"value flag last":    base("--mtu"),
	}
	for name, args := range bad {
		if err := ValidateUpArgs(args); err == nil {
			t.Errorf("%s: accepted\n%q", name, args)
		}
	}
}

func TestEveryAllowedFlagIsExercised(t *testing.T) {
	// A new entry in the allowlist has to come with a decision about what
	// values are safe. This fails until the table above is extended.
	covered := map[string]bool{}
	for _, args := range [][]string{
		base("--org", "o"), base("--mtu", "1400"), base("--netstack-dns", "1.1.1.1"), base("--interface-name", "p0"),
		base("--log-level", "info"), base("--ping-interval", "1s"), base("--ping-timeout", "1s"),
		base("--upstream-dns", "1.1.1.1"), base("--match-domains", "a"), base("--exit-node-site-ids", "1"),
		base("--exit-node-resource-id", "1"), base("--holepunch"), base("--override-dns"), base("--tunnel-dns"),
		base("--prefer-local-routes"), base("--exit-node-takes-precedence"), base("--disable-relay"),
	} {
		if err := ValidateUpArgs(args); err != nil {
			t.Fatalf("%q: %v", args, err)
		}
		for _, a := range args[2:] {
			if strings.HasPrefix(a, "--") {
				name, _, _ := strings.Cut(a[2:], "=")
				covered[name] = true
			}
		}
	}
	for name := range allowed {
		if !covered[name] {
			t.Errorf("--%s has no validation test", name)
		}
	}
}

func TestValidateUpArgsDoesNotMutate(t *testing.T) {
	args := base("--org", "o", "--holepunch=false")
	want := slices.Clone(args)
	if err := ValidateUpArgs(args); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(args, want) {
		t.Fatalf("changed %q", args)
	}
}

// pangolinParse reads validated arguments the way the CLI's flag package
// does: a value flag takes the next word, whatever it looks like.
func pangolinParse(args []string) map[string]string {
	out := map[string]string{}
	for i := 2; i < len(args); i++ {
		name, value, hasValue := strings.Cut(strings.TrimPrefix(args[i], "--"), "=")
		spec := allowed[name]
		switch {
		case hasValue:
			out[name] = value
		case spec.kind == boolFlag:
			out[name] = "true"
		default:
			i++
			out[name] = args[i]
		}
	}
	return out
}

func TestMoveCredentialsKeepsFlagMeaning(t *testing.T) {
	// "--org --id" is a valid way to set the organization to "--id". The
	// helper must not mistake that value for the --id flag.
	args := []string{"up", "client", "--org", "--id", "--id", "client-1", "--secret", "s", "--endpoint", "https://p.example"}
	if err := ValidateUpArgs(args); err != nil {
		t.Fatalf("test input should validate: %v", err)
	}
	rest, env := moveCredentials(args)
	got := pangolinParse(rest)
	for _, e := range env {
		k, v, _ := strings.Cut(e, "=")
		got[map[string]string{"PANGOLIN_CLIENT_ID": "id", "PANGOLIN_CLIENT_SECRET": "secret"}[k]] = v
	}
	if want := pangolinParse(args); !mapsEqual(got, want) {
		t.Fatalf("meaning changed\n before %v\n after  %v\n rest %q env %q", want, got, rest, env)
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

func TestMoveCredentialsPreservesMeaningOfRandomArguments(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	required := [][][]string{
		{{"--id", "x"}, {"--id=x"}},
		{{"--secret", "y"}, {"--secret=y"}},
		{{"--endpoint", "https://p.example"}, {"--endpoint=https://p.example"}},
	}
	extra := [][]string{
		{"--org", "--id"}, {"--org", "--secret"}, {"--org=--id"}, {"--org", "o"}, {"--mtu", "1400"},
		{"--holepunch"}, {"--tunnel-dns=false"}, {"--netstack-dns", "--secret"}, {"--log-level", "info"},
		{"--ping-interval", "--id"}, {"--upstream-dns", "--secret"}, {"--interface-name", "--id"},
	}
	checked := 0
	for range 50000 {
		var units [][]string
		for _, forms := range required {
			units = append(units, forms[rng.Intn(len(forms))])
		}
		for range rng.Intn(5) {
			units = append(units, extra[rng.Intn(len(extra))])
		}
		rng.Shuffle(len(units), func(i, j int) { units[i], units[j] = units[j], units[i] })
		args := []string{"up", "client"}
		for _, u := range units {
			args = append(args, u...)
		}
		if ValidateUpArgs(args) != nil {
			continue
		}
		checked++
		rest, env := moveCredentials(args)
		got := pangolinParse(rest)
		for _, e := range env {
			k, v, _ := strings.Cut(e, "=")
			got[map[string]string{"PANGOLIN_CLIENT_ID": "id", "PANGOLIN_CLIENT_SECRET": "secret"}[k]] = v
		}
		if want := pangolinParse(args); !mapsEqual(got, want) {
			t.Fatalf("meaning changed for %q\n before %v\n after  %v\n rest %q env %q", args, want, got, rest, env)
		}
	}
	if checked < 1000 {
		t.Fatalf("only %d random argument lists were valid, the generator is too weak", checked)
	}
}

func TestParseShellRoundTripsArbitraryWords(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	alphabet := []rune("abc XYZ019\"'\\`$;&|<>(){}[]*?!#~%\n\t\r\x00\x7f\u00e9\u65e5\U0001f600\u2028 ")
	word := func() string {
		var b strings.Builder
		for range rng.Intn(14) {
			if rng.Intn(20) == 0 {
				b.WriteByte(byte(0x80 + rng.Intn(0x80))) // invalid UTF-8
				continue
			}
			b.WriteRune(alphabet[rng.Intn(len(alphabet))])
		}
		return b.String()
	}
	for range 3000 {
		words := []string{word() + "/pangolin", "up", "client"}
		for range rng.Intn(6) {
			words = append(words, word())
		}
		keyring := rng.Intn(2) == 0
		l, err := ParseShell(cliScript(keyring, words...))
		if err != nil {
			t.Fatalf("%q: %v", words, err)
		}
		if l.Keyring != keyring || l.Exe != words[0] || !slices.Equal(l.Args, words[1:]) {
			t.Fatalf("round trip lost data\n in  %q\n out %q %q", words, l.Exe, l.Args)
		}
	}
}

func TestParseShellEdgeCases(t *testing.T) {
	good := cliScript(false, "/usr/bin/pangolin", "up", "client")
	for name, tc := range map[string]struct {
		script string
		ok     bool
	}{
		"plain":                  {good, true},
		"surrounding space":      {"  " + good + "\n", true},
		"two spaces between":     {strings.Replace(good, `"up"`, ` "up"`, 1), true},
		"only two words":         {cliScript(false, "/usr/bin/pangolin", "up"), false},
		"no words":               {cliScript(false), false},
		"missing prefix":         {strings.TrimPrefix(good, "export PANGOLIN_SUBPROCESS=1 && "), false},
		"wrong prefix value":     {strings.Replace(good, "SUBPROCESS=1", "SUBPROCESS=0", 1), false},
		"keyring twice":          {strings.Replace(cliScript(true, "/x/p", "up", "client"), "nohup", "export PANGOLIN_CREDENTIALS_FROM_KEYRING=1 && nohup", 1), false},
		"other export":           {strings.Replace(good, "nohup", "export LD_PRELOAD=/tmp/x.so && nohup", 1), false},
		"missing redirect":       {strings.TrimSuffix(good, " >/dev/null 2>&1 &"), false},
		"different redirect":     {strings.Replace(good, ">/dev/null", ">/tmp/out", 1), false},
		"foreground":             {strings.TrimSuffix(good, " &"), false},
		"command chained":        {good + " id", false},
		"semicolon after":        {strings.Replace(good, " >/dev", "; id >/dev", 1), false},
		"unquoted word":          {strings.Replace(good, `"client"`, "client", 1), false},
		"single quotes":          {strings.ReplaceAll(good, `"`, "'"), false},
		"glued words":            {strings.Replace(good, `"up" "client"`, `"up""client"`, 1), false},
		"bad escape":             {strings.Replace(good, `"client"`, `"cli\qent"`, 1), false},
		"escaped final quote":    {strings.Replace(good, `"client"`, `"client\"`, 1), false},
		"literal newline in str": {strings.Replace(good, `"client"`, "\"cli\nent\"", 1), false},
		"tab separator":          {strings.Replace(good, `"up" "client"`, "\"up\"\t\"client\"", 1), false},
		"empty":                  {"", false},
		"only prefix":            {"export PANGOLIN_SUBPROCESS=1 && ", false},
	} {
		_, err := ParseShell(tc.script)
		if (err == nil) != tc.ok {
			t.Errorf("%s: err = %v, want ok = %v\n%q", name, err, tc.ok, tc.script)
		}
		if err != nil && !errors.Is(err, ErrUnsupported) {
			t.Errorf("%s: error is not ErrUnsupported: %v", name, err)
		}
	}
}

func TestParseSudoArgumentShapes(t *testing.T) {
	good := cliScript(false, "/usr/bin/pangolin", "up", "client")
	for name, args := range map[string][]string{
		"none":         nil,
		"one":          {"sh"},
		"four":         {"sh", "-c", good, "extra"},
		"preceding -E": {"-E", "sh", "-c", good},
		"-u root":      {"-u", "root", "sh", "-c", good},
		"login shell":  {"sh", "-lc", good},
		"bash":         {"bash", "-c", good},
		"abs sh":       {"/bin/sh", "-c", good},
	} {
		if _, err := ParseSudo(args); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func FuzzParseShell(f *testing.F) {
	f.Add(cliScript(true, upWords...))
	f.Add(cliScript(false, "/usr/bin/pangolin", "up", "client"))
	f.Add(`export PANGOLIN_SUBPROCESS=1 && nohup "a" "b" "c\x00" >/dev/null 2>&1 &`)
	f.Add("")
	f.Fuzz(func(t *testing.T, script string) {
		l, err := ParseShell(script)
		if err != nil {
			return
		}
		if len(l.Args) < 2 {
			t.Fatalf("too few args accepted: %q", script)
		}
		again, err := ParseShell(cliScript(l.Keyring, append([]string{l.Exe}, l.Args...)...))
		if err != nil || again.Exe != l.Exe || !slices.Equal(again.Args, l.Args) || again.Keyring != l.Keyring {
			t.Fatalf("not stable: %q -> %+v -> %+v (%v)", script, l, again, err)
		}
	})
}

func FuzzValidateUpArgs(f *testing.F) {
	f.Add("up\x00client\x00--id\x00a\x00--secret\x00b\x00--endpoint\x00https://p.example")
	f.Add("up\x00client\x00--org\x00--id\x00--id\x00a\x00--secret\x00b\x00--endpoint\x00https://p.example")
	f.Fuzz(func(t *testing.T, joined string) {
		args := strings.Split(joined, "\x00")
		if ValidateUpArgs(args) != nil {
			return
		}
		// Read the accepted arguments the way the CLI will. Every flag it
		// sees must be allowed and every value must pass that flag's check.
		for name, value := range pangolinParse(args) {
			spec, ok := allowed[name]
			if !ok {
				t.Fatalf("flag --%s got through: %q", name, args)
			}
			if spec.kind == valueFlag && spec.check(value) != nil {
				t.Fatalf("--%s=%q got through: %q", name, value, args)
			}
		}
		m := pangolinParse(args)
		for _, r := range []string{"id", "secret", "endpoint"} {
			if _, ok := m[r]; !ok {
				t.Fatalf("--%s missing but accepted: %q", r, args)
			}
		}
	})
}

func TestShimDirRefusesLooseParents(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("TMPDIR", tmp)
	parent := filepath.Join(tmp, "traygolin-"+strconv.Itoa(os.Getuid()))
	if err := os.Mkdir(parent, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o777); err != nil {
		t.Fatal(err)
	}
	// Anyone could rename the directories below a world-writable parent and
	// swap in their own "sudo".
	if dir, err := ShimDir("/usr/bin/traygolin"); err == nil {
		t.Fatalf("accepted a shim dir below a world-writable parent: %s", dir)
	}
}

func TestShimDirFallbackIsPrivate(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("TMPDIR", tmp)
	dir, err := ShimDir("/usr/bin/traygolin")
	if err != nil {
		t.Fatal(err)
	}
	for d := dir; d != tmp; d = filepath.Dir(d) {
		info, err := os.Stat(d)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s is %v", d, info.Mode().Perm())
		}
	}
}

func TestShimDirErrors(t *testing.T) {
	rt := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", rt)
	// A regular file where the directory should be.
	if err := os.WriteFile(filepath.Join(rt, "traygolin"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ShimDir("/usr/bin/traygolin"); err == nil {
		t.Error("a file in the way should be an error")
	}
	// A directory in place of the link.
	base2 := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", base2)
	if err := os.MkdirAll(filepath.Join(base2, "traygolin", "bin", "sudo", "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := ShimDir("/usr/bin/traygolin"); err == nil {
		t.Error("a non-empty directory where the link goes should be an error")
	}
}

func TestShimDirConcurrent(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			self := "/usr/bin/traygolin"
			if i%2 == 1 {
				self = "/opt/traygolin"
			}
			if _, err := ShimDir(self); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		// Two launches racing for the same link must not fail the second.
		t.Errorf("concurrent ShimDir: %v", err)
	}
}

func TestTrustedBinaryEdgeCases(t *testing.T) {
	uid := os.Getuid()
	if _, err := TrustedBinary(nil, uid); err == nil {
		t.Error("no candidates")
	}
	if _, err := TrustedBinary([]string{""}, uid); err == nil {
		t.Error("empty candidate")
	}
	dir := t.TempDir()
	os.Chmod(dir, 0o755)
	// A directory is not a binary.
	sub := filepath.Join(dir, "pangolin")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := TrustedBinary([]string{sub}, uid); err == nil {
		t.Error("directory accepted as binary")
	}
	// A broken link.
	dangling := filepath.Join(dir, "dangling")
	os.Symlink(filepath.Join(dir, "nowhere"), dangling)
	if _, err := TrustedBinary([]string{dangling}, uid); err == nil {
		t.Error("dangling link accepted")
	}
	// Group-writable (0775) is as bad as world-writable.
	gw := filepath.Join(dir, "gw")
	os.WriteFile(gw, []byte("x"), 0o755)
	os.Chmod(gw, 0o775)
	if _, err := TrustedBinary([]string{gw}, uid); err == nil {
		t.Error("group-writable binary accepted")
	}
	gdir := filepath.Join(t.TempDir(), "g")
	os.Mkdir(gdir, 0o775)
	os.Chmod(gdir, 0o775)
	inG := filepath.Join(gdir, "pangolin")
	os.WriteFile(inG, []byte("x"), 0o755)
	if _, err := TrustedBinary([]string{inG}, uid); err == nil {
		t.Error("binary in a group-writable directory accepted")
	}
	// A link that points into a loose directory is judged by its target.
	viaLink := filepath.Join(dir, "via")
	os.Symlink(inG, viaLink)
	if _, err := TrustedBinary([]string{viaLink}, uid); err == nil {
		t.Error("link into a loose directory accepted")
	}
	// A link loop.
	loopA, loopB := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	os.Symlink(loopB, loopA)
	os.Symlink(loopA, loopB)
	if _, err := TrustedBinary([]string{loopA}, uid); err == nil {
		t.Error("link loop accepted")
	}
	// A relative symlink resolves to the real, trusted file.
	real := filepath.Join(dir, "real")
	os.WriteFile(real, []byte("x"), 0o755)
	rel := filepath.Join(dir, "rel")
	os.Symlink("real", rel)
	if got, err := TrustedBinary([]string{rel}, uid); err != nil || got != real {
		t.Errorf("relative link: %q %v", got, err)
	}
	// Order matters: the first trustworthy candidate wins.
	other := filepath.Join(dir, "other")
	os.WriteFile(other, []byte("x"), 0o755)
	if got, _ := TrustedBinary([]string{other, real}, uid); got != other {
		t.Errorf("order: %q", got)
	}
	// Errors from every candidate are reported.
	_, err := TrustedBinary([]string{gw, dangling}, uid)
	if err == nil || !strings.Contains(err.Error(), "gw") || !strings.Contains(err.Error(), "nowhere") {
		t.Errorf("error should mention every rejected candidate: %v", err)
	}
	// The mode check ignores the executable bits and the sticky/setuid bits.
	suid := filepath.Join(dir, "suid")
	os.WriteFile(suid, []byte("x"), 0o755)
	os.Chmod(suid, 0o755|os.ModeSetuid)
	if _, err := TrustedBinary([]string{suid}, uid); err != nil {
		t.Errorf("setuid bit should not matter to the writable check: %v", err)
	}
}

func TestRestrictSocketTimesOutQuietly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "never.sock")
	start := time.Now()
	if err := restrictSocket(path, fileID{}, os.Getuid(), 200*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("did not honor the timeout")
	}
}

func TestRestrictSocketIgnoresNonSockets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "olm.sock")
	if err := os.WriteFile(path, nil, 0o666); err != nil {
		t.Fatal(err)
	}
	os.Chmod(path, 0o666)
	// A regular file or a link at the socket path must not be chowned or
	// chmodded: root following a planted link would be a privilege bug.
	if err := restrictSocket(path, fileID{}, os.Getuid(), 150*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o666 {
		t.Errorf("regular file was modified: %v", info.Mode())
	}
}

func TestRestrictSocketDoesNotFollowLinks(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim")
	if err := os.WriteFile(victim, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "olm.sock")
	if err := os.Symlink(victim, link); err != nil {
		t.Fatal(err)
	}
	if err := restrictSocket(link, fileID{}, os.Getuid(), 150*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(victim); info.Mode().Perm() != 0o644 {
		t.Errorf("link target was modified: %v", info.Mode())
	}
}

func TestSocketIDOfMissingPath(t *testing.T) {
	if socketID(filepath.Join(t.TempDir(), "x")) != (fileID{}) {
		t.Error("missing path should give the zero id")
	}
}

func TestRunHelperRefusesWhenNotInstalled(t *testing.T) {
	// HelperPath is pointed at nothing so that a real helper on a developer
	// machine can never be started by a test.
	old := HelperPath
	HelperPath = filepath.Join(t.TempDir(), "no-helper")
	t.Cleanup(func() { HelperPath = old })
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for name, fn := range map[string]func(context.Context) error{"down": PrivilegedDown, "reset-dns": ResetDNS} {
		if err := fn(ctx); err == nil || !strings.Contains(err.Error(), "not installed") {
			t.Errorf("%s: %v", name, err)
		}
	}
	if Installed() || Ready() {
		t.Error("nothing is installed")
	}
}

func TestSetupWithoutHelperBinary(t *testing.T) {
	err := Setup(t.Context(), filepath.Join(t.TempDir(), "missing"), []byte("@HELPER@"))
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("%v", err)
	}
}

func TestRunShimWithoutHelper(t *testing.T) {
	old := HelperPath
	HelperPath = filepath.Join(t.TempDir(), "no-helper")
	t.Cleanup(func() { HelperPath = old })
	var out strings.Builder
	code := RunShim([]string{"sh", "-c", cliScript(true, upWords...)}, &out)
	if code == 0 || !strings.Contains(out.String(), "not installed") {
		t.Errorf("code %d: %s", code, out.String())
	}
	if strings.Contains(out.String(), "s3cr") {
		t.Errorf("the secret was printed: %s", out.String())
	}
}

func TestRenderPolicyEdgeCases(t *testing.T) {
	old := HelperPath
	HelperPath = "/opt/a b/traygolin-helper"
	t.Cleanup(func() { HelperPath = old })
	got := string(RenderPolicy([]byte(`<a>@HELPER@</a><b>@HELPER@</b>`)))
	if got != `<a>/opt/a b/traygolin-helper</a><b>/opt/a b/traygolin-helper</b>` {
		t.Errorf("%q", got)
	}
	if string(RenderPolicy(nil)) != "" {
		t.Error("nil template")
	}
}

func TestHelperBesides(t *testing.T) {
	for in, want := range map[string]string{
		"/usr/bin/traygolin": "/usr/bin/traygolin-helper",
		"traygolin":          "traygolin-helper",
		"/a b/c/traygolin":   "/a b/c/traygolin-helper",
		"/usr/bin/../bin/tg": "/usr/bin/traygolin-helper",
		"/":                  "/traygolin-helper",
		"./relative/tg":      "relative/traygolin-helper",
	} {
		if got := HelperBesides(in); got != want {
			t.Errorf("HelperBesides(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTunnelCommandEdgeCases(t *testing.T) {
	dir := t.TempDir()
	run := filepath.Join(dir, "systemd-run")
	if err := os.WriteFile(run, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	args := []string{"up", "client", "--id", "a b"}
	if got := tunnelCommand("/usr/bin/pangolin", args, false, []string{run}); !slices.Equal(got, append([]string{"/usr/bin/pangolin"}, args...)) {
		t.Errorf("not booted: %q", got)
	}
	got := tunnelCommand("/usr/bin/pangolin", args, true, []string{filepath.Join(dir, "nope"), dir, run})
	if got[0] != run || !slices.Contains(got, "--scope") {
		t.Errorf("should use the first regular systemd-run: %q", got)
	}
	sep := slices.Index(got, "--")
	if sep < 0 || got[sep+1] != "/usr/bin/pangolin" || !slices.Equal(got[sep+2:], args) {
		t.Errorf("options must end before the command: %q", got)
	}
	if got := tunnelCommand("/p", nil, true, nil); !slices.Equal(got, []string{"/p"}) {
		t.Errorf("no systemd-run: %q", got)
	}
}

func TestUpEnvEdgeCases(t *testing.T) {
	u := &user.User{Username: "alice", Uid: "1000", Gid: "1000"}
	t.Setenv("LANG", "de_DE.UTF-8")
	if !slices.Contains(upEnv(u, false), "LANG=de_DE.UTF-8") {
		t.Error("LANG is passed through")
	}
	t.Setenv("LANG", "x\nPATH=/evil")
	for _, e := range upEnv(u, false) {
		if strings.HasPrefix(e, "LANG=") {
			t.Errorf("a LANG with a newline is passed on: %q", e)
		}
	}
	t.Setenv("LANG", "")
	for _, e := range upEnv(u, false) {
		if strings.HasPrefix(e, "LANG=") {
			t.Errorf("empty LANG should be left out: %q", e)
		}
	}
	// Whatever the caller's environment holds, the root process gets a fixed
	// PATH and HOME.
	t.Setenv("PATH", "/tmp/evil")
	t.Setenv("LD_PRELOAD", "/tmp/evil.so")
	for _, e := range upEnv(u, true) {
		if strings.HasPrefix(e, "LD_") || strings.Contains(e, "/tmp/evil") {
			t.Errorf("leaked caller environment: %q", e)
		}
	}
}

func TestPkexecCaller(t *testing.T) {
	t.Setenv("PKEXEC_UID", "")
	if _, err := pkexecCaller(); err == nil {
		t.Error("unset")
	}
	for _, bad := range []string{"abc", "-1x", "1;id", " 1", "1 ", "0x10"} {
		t.Setenv("PKEXEC_UID", bad)
		if _, err := pkexecCaller(); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	t.Setenv("PKEXEC_UID", strconv.Itoa(os.Getuid()))
	u, err := pkexecCaller()
	if err != nil || u.Uid != strconv.Itoa(os.Getuid()) {
		t.Errorf("own uid: %v %v", u, err)
	}
	t.Setenv("PKEXEC_UID", "4294967000")
	if _, err := pkexecCaller(); err == nil {
		t.Error("unknown uid")
	}
}

func TestSetupScriptIsPOSIX(t *testing.T) {
	if strings.Contains(setupScript, "[[") || strings.Contains(setupScript, "==") {
		t.Error("the script runs under /bin/sh")
	}
	if !utf8.ValidString(setupScript) {
		t.Error("invalid UTF-8")
	}
}

func TestValidateConcurrent(t *testing.T) {
	// The validator keeps no shared state.
	var wg sync.WaitGroup
	for i := range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			args := base("--org", fmt.Sprintf("org%d", i))
			if i%2 == 1 {
				args = append(args, "--mtu", "1")
			}
			if err := ValidateUpArgs(args); (err == nil) != (i%2 == 0) {
				t.Errorf("%d: %v", i, err)
			}
		}()
	}
	wg.Wait()
}
