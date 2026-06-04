package scan

import (
	"strings"
	"testing"

	"github.com/doguyilmaz/dothaven/internal/snapshot"
)

const ghp = "ghp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" // ghp_ + 36

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
