package scan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/doguyilmaz/dothaven/internal/snapshot"
)

const ghp = "ghp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" // ghp_ + 36

func TestScanDirSkipsAndScans(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "secret.env"), "TOKEN="+ghp)
	mustWrite(t, filepath.Join(dir, "clean.txt"), "theme = dark")
	// a pruned subtree whose secret must never be scanned
	if err := os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "node_modules", "leak.env"), "TOKEN="+ghp)

	var scanned int64
	results, err := ScanDir(context.Background(), dir, &scanned)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, r := range results {
		if strings.Contains(r.Path, "node_modules") {
			t.Errorf("node_modules subtree should be pruned, got %s", r.Path)
		}
	}
	found := false
	for _, r := range results {
		if strings.HasSuffix(r.Path, "secret.env") && r.Action == Redact {
			found = true
		}
	}
	if !found {
		t.Errorf("secret.env should be flagged for redaction; results: %+v", results)
	}
	if scanned < 2 {
		t.Errorf("progress counter = %d, want >= 2 files scanned", scanned)
	}
}

func TestScanDirCancelled(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.txt"), "x")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ScanDir(ctx, dir, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled scan err = %v, want context.Canceled", err)
	}
}

func TestScanFileSkipsNonRegular(t *testing.T) {
	if ScanFile(t.TempDir()) != nil {
		t.Error("ScanFile should return nil for a directory (non-regular)")
	}
}

func TestScanDirFollowsSymlinkToRegularFile(t *testing.T) {
	// A symlink to a regular file must still be scanned (os.Stat follows it);
	// only symlinks to devices/FIFOs are skipped. Regression guard: a DirEntry
	// reports a symlink as non-regular, so a naive type check would drop it.
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "real_secret")
	mustWrite(t, target, "TOKEN="+ghp)
	if err := os.Symlink(target, filepath.Join(dir, "link.env")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	results, err := ScanDir(context.Background(), dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, r := range results {
		if strings.HasSuffix(r.Path, "link.env") && r.Action == Redact {
			found = true
		}
	}
	if !found {
		t.Errorf("secret behind a symlink-to-regular should be scanned; results: %+v", results)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestScanContentActions(t *testing.T) {
	cases := []struct {
		name, content string
		want          Action
		patternID     string
	}{
		{"private key", "-----BEGIN OPENSSH PRIVATE KEY-----", Skip, "private-key-pem"},
		{"github token", "export GITHUB_TOKEN=" + ghp, Redact, ""},
		{"ip", "HostName 10.0.12.34", Redact, "ip-address"},
		{"email", "  email = dev@example.com", Include, "email-address"},
		{"clean", "theme = dark", Include, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := ScanContent("x", c.content)
			if r.Action != c.want {
				t.Errorf("action = %s, want %s (findings: %+v)", r.Action, c.want, r.Findings)
			}
			if c.patternID != "" {
				found := false
				for _, f := range r.Findings {
					if f.Pattern.ID == c.patternID {
						found = true
					}
				}
				if !found {
					t.Errorf("expected pattern %q among findings", c.patternID)
				}
			}
		})
	}
}

func TestScanContentActionPriority(t *testing.T) {
	// a private key (skip) wins over a token (redact) on the same content
	r := ScanContent("x", "TOKEN=ghp_x\n-----BEGIN RSA PRIVATE KEY-----")
	if r.Action != Skip {
		t.Errorf("skip should win, got %s", r.Action)
	}
}

func TestApplyRedactions(t *testing.T) {
	content := "a = " + ghp + "\nb = " + ghp
	r := ScanContent("x", content)
	out := ApplyRedactions(content, r)
	if strings.Contains(out, ghp) {
		t.Errorf("token not redacted: %s", out)
	}
	if !strings.Contains(out, Marker) {
		t.Errorf("missing marker: %s", out)
	}
}

func TestRedactSectionContentSkip(t *testing.T) {
	pk := "-----BEGIN OPENSSH PRIVATE KEY-----\nbase64"
	s := snapshot.Section{Content: &pk}
	kept, _ := RedactSection("ssh.key", &s)
	if kept {
		t.Error("private-key content should drop the section (kept=false)")
	}
}

func TestRedactSectionPairs(t *testing.T) {
	s := snapshot.Section{Pairs: map[string]string{
		"apiKey": "sk-ant-api03-abcdefghijklmnopqrstuvwxyz",
		"theme":  "dark",
	}}
	RedactSection("ai.x", &s)
	if s.Pairs["apiKey"] != Marker {
		t.Errorf("secret value should be redacted, got %q", s.Pairs["apiKey"])
	}
	if s.Pairs["theme"] != "dark" {
		t.Errorf("benign value changed: %q", s.Pairs["theme"])
	}
}

func TestRedactSectionSecretKey(t *testing.T) {
	s := snapshot.Section{Pairs: map[string]string{
		"GITHUB_TOKEN=" + ghp: "enabled",
		"theme":               "dark",
	}}
	RedactSection("ai.x", &s)
	if _, ok := s.Pairs["GITHUB_TOKEN="+ghp]; ok {
		t.Error("secret-bearing key should be dropped entirely")
	}
	if s.Pairs["theme"] != "dark" {
		t.Errorf("benign pair lost: %v", s.Pairs)
	}
}

func TestRedactSectionItems(t *testing.T) {
	s := snapshot.Section{Items: []snapshot.Item{
		{Raw: "TOKEN=" + ghp, Columns: []string{"TOKEN", ghp}},
		{Raw: "normal.txt", Columns: []string{"normal.txt"}},
	}}
	RedactSection("x", &s)
	if s.Items[0].Raw != Marker || s.Items[0].Columns[0] != Marker || s.Items[0].Columns[1] != Marker {
		t.Errorf("secret item not fully redacted: %+v", s.Items[0])
	}
	if s.Items[1].Raw != "normal.txt" {
		t.Errorf("benign item changed: %+v", s.Items[1])
	}
}

func TestTargetedRedactors(t *testing.T) {
	if got := RedactNpmTokens("//registry/:_authToken=npm_secret123"); !strings.Contains(got, "_authToken="+Marker) || strings.Contains(got, "npm_secret123") {
		t.Errorf("npm token: %q", got)
	}
	if got := RedactSSHConfig("Host x\n  HostName 1.2.3.4\n  IdentityFile ~/.ssh/id"); strings.Contains(got, "1.2.3.4") || strings.Contains(got, "~/.ssh/id") {
		t.Errorf("ssh config not redacted: %q", got)
	}
	if got := RedactIPs("ip 10.0.0.1 here"); strings.Contains(got, "10.0.0.1") {
		t.Errorf("ip not redacted: %q", got)
	}
}

func TestRedactSSHConfigCaseInsensitive(t *testing.T) {
	// ssh_config keywords are case-insensitive; the lowercase forms are valid
	// syntax and must redact too (regression: they used to leak verbatim).
	in := "Host work\n  hostname secret.example.com\n  identityfile ~/.ssh/id_corp"
	got := RedactSSHConfig(in)
	if strings.Contains(got, "secret.example.com") || strings.Contains(got, "id_corp") {
		t.Errorf("lowercase ssh keywords leaked: %q", got)
	}
}

func TestRedactNpmLegacyAuth(t *testing.T) {
	// Legacy _auth= / _password= (base64) lines, not just _authToken=, must scrub.
	cases := map[string]string{
		"_auth=aGVsbG86d29ybGQ=":                                "aGVsbG86d29ybGQ=",
		"_password=c3VwZXJzZWNyZXQ=":                            "c3VwZXJzZWNyZXQ=",
		"//registry.npmjs.org/:_authToken=npm_tokenvalue123abc": "npm_tokenvalue123abc",
	}
	for line, secret := range cases {
		got := RedactNpmTokens(line)
		if strings.Contains(got, secret) {
			t.Errorf("npm auth value leaked: %q", got)
		}
		if !strings.Contains(got, Marker) {
			t.Errorf("expected marker in %q", got)
		}
	}
}

func TestScanDetectsLegacyNpmAuth(t *testing.T) {
	for _, line := range []string{"_auth=YWxpY2U6c2VjcmV0", "_password=c2VjcmV0dmFsdWU="} {
		if r := ScanContent("npmrc", line); r.Action != Redact {
			t.Errorf("%q: action=%s, want redact", line, r.Action)
		}
	}
}

func hasFinding(r Result, id string) bool {
	for _, f := range r.Findings {
		if f.Pattern.ID == id {
			return true
		}
	}
	return false
}

func TestPgpassMultilineRedaction(t *testing.T) {
	// (?m) regression: per-line detect AND whole-text redact must both fire.
	in := "localhost:5432:db:alice:secretA\nprod:5432:db:bob:secretB"
	r := ScanContent("pgpass", in)
	if r.Action != Redact {
		t.Fatalf("action=%s, want redact", r.Action)
	}
	if out := ApplyRedactions(in, r); strings.Contains(out, "secretA") || strings.Contains(out, "secretB") {
		t.Errorf("pgpass credentials leaked: %q", out)
	}
}

func TestPgpassIgnoresBenignColonLines(t *testing.T) {
	for _, line := range []string{
		"export PATH=/a:/b:/c:/d:/e/bin",
		"PATH=/usr/local/bin:/usr/bin:/bin:/sbin:/usr/sbin",
		"fe80::1ff:fe23:4567:890a",
		"root:x:0:0:root:/root:/bin/bash",
	} {
		if hasFinding(ScanContent("rc", line), "pgpass-line") {
			t.Errorf("benign colon line flagged as pgpass: %q", line)
		}
	}
	// a genuine pgpass line still matches
	if !hasFinding(ScanContent("pgpass", "db.host:5432:app:user:pw"), "pgpass-line") {
		t.Error("real pgpass line not detected")
	}
}

func TestBearerTokenNoProseFalsePositive(t *testing.T) {
	if hasFinding(ScanContent("readme", "Use the Bearer token scheme in your header."), "bearer-token") {
		t.Error("prose 'Bearer token' falsely flagged as a bearer token")
	}
	if !hasFinding(ScanContent("h", "Authorization: Bearer abcdefghijklmnopqrstuvwxyz0123456789"), "bearer-token") {
		t.Error("real bearer token not detected")
	}
}

func TestIPAddressBoundsOctets(t *testing.T) {
	if got := RedactIPs("build 1.2.3.400 done"); got != "build 1.2.3.400 done" {
		t.Errorf("out-of-range octet should not be treated as an IP: %q", got)
	}
	if got := RedactIPs("host 10.0.0.1"); strings.Contains(got, "10.0.0.1") {
		t.Errorf("valid IP not redacted: %q", got)
	}
}

func TestFormatSecurityReport(t *testing.T) {
	clean := FormatSecurityReport([]Result{ScanContent("a", "theme = dark")})
	if !strings.Contains(clean, "No sensitive data found") || !strings.Contains(clean, "1 file(s) scanned") {
		t.Errorf("clean report wrong:\n%s", clean)
	}
	withHits := FormatSecurityReport([]Result{
		ScanContent("~/.ssh/id_rsa", "-----BEGIN RSA PRIVATE KEY-----"),
		ScanContent("~/.gitconfig", "  email = dev@example.com"),
	})
	for _, want := range []string{"## 🔴 HIGH", "`~/.ssh/id_rsa`", "skip (private key)", "## 🟡 MEDIUM", "`~/.gitconfig`"} {
		if !strings.Contains(withHits, want) {
			t.Errorf("report missing %q:\n%s", want, withHits)
		}
	}
}
