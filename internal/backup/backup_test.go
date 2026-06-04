package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/doguyilmaz/dothaven/internal/registry"
	"github.com/doguyilmaz/dothaven/internal/scan"
)

func TestSelected(t *testing.T) {
	cases := []struct {
		cat        string
		only, skip []string
		want       bool
	}{
		{"shell", nil, nil, true},
		{"shell", []string{"shell", "git"}, nil, true},
		{"npm", []string{"shell", "git"}, nil, false},
		{"shell", nil, []string{"shell"}, false},
		{"shell", []string{"shell"}, []string{"shell"}, false}, // skip wins
	}
	for _, c := range cases {
		if got := selected(c.cat, c.only, c.skip); got != c.want {
			t.Errorf("selected(%q, only=%v, skip=%v) = %v, want %v", c.cat, c.only, c.skip, got, c.want)
		}
	}
}

func TestRunRedactsAndGates(t *testing.T) {
	home := t.TempDir()
	dest := t.TempDir()

	mustWrite(t, filepath.Join(home, ".zshrc"), "alias ll='ls -la'\n")
	mustWrite(t, filepath.Join(home, ".npmrc"), "//registry.npmjs.org/:_authToken=npm_supersecret123456789012345\n")
	mustWrite(t, filepath.Join(home, "id_ed25519"), "-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----\n")

	targets := []registry.BackupTarget{
		{Src: filepath.Join(home, ".zshrc"), Dest: "shell/.zshrc", Category: "shell"},
		{Src: filepath.Join(home, ".npmrc"), Dest: "npm/.npmrc", Category: "npm", Redact: scan.RedactNpmTokens},
		{Src: filepath.Join(home, "id_ed25519"), Dest: "ssh/id_ed25519", Category: "ssh"},
		{Src: filepath.Join(home, "missing"), Dest: "x/missing", Category: "shell"}, // absent → skipped
	}

	res, err := Run(targets, dest, Options{Redact: true})
	if err != nil {
		t.Fatal(err)
	}

	// zshrc copied verbatim
	if got := readFile(t, filepath.Join(dest, "shell/.zshrc")); !strings.Contains(got, "alias ll") {
		t.Errorf("zshrc not backed up: %q", got)
	}
	// npmrc token redacted
	npm := readFile(t, filepath.Join(dest, "npm/.npmrc"))
	if strings.Contains(npm, "npm_supersecret123456789012345") || !strings.Contains(npm, scan.Marker) {
		t.Errorf("npmrc not redacted: %q", npm)
	}
	// private key NOT copied (skip gate)
	if _, err := os.Stat(filepath.Join(dest, "ssh/id_ed25519")); !os.IsNotExist(err) {
		t.Error("private key should be skipped, not backed up")
	}

	if res.TotalFiles != 2 {
		t.Errorf("TotalFiles = %d, want 2", res.TotalFiles)
	}
	if res.PerCategory["shell"] != 1 || res.PerCategory["npm"] != 1 {
		t.Errorf("PerCategory = %v", res.PerCategory)
	}
}

func TestRunNoRedactKeepsRaw(t *testing.T) {
	home := t.TempDir()
	dest := t.TempDir()
	mustWrite(t, filepath.Join(home, "id_rsa"), "-----BEGIN RSA PRIVATE KEY-----\nx\n-----END RSA PRIVATE KEY-----\n")
	targets := []registry.BackupTarget{{Src: filepath.Join(home, "id_rsa"), Dest: "ssh/id_rsa", Category: "ssh"}}

	if _, err := Run(targets, dest, Options{Redact: false}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(dest, "ssh/id_rsa")); !strings.Contains(got, "BEGIN RSA PRIVATE KEY") {
		t.Errorf("--no-redact should copy raw key, got %q", got)
	}
}

func TestRunDir(t *testing.T) {
	home := t.TempDir()
	dest := t.TempDir()
	skills := filepath.Join(home, ".claude", "skills")
	mustWrite(t, filepath.Join(skills, "a.md"), "skill a\n")
	mustWrite(t, filepath.Join(skills, "nested", "b.md"), "skill b\n")

	targets := []registry.BackupTarget{{Src: skills, Dest: "ai/claude/skills", Category: "ai", IsDir: true}}
	res, err := Run(targets, dest, Options{Redact: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.TotalFiles != 2 {
		t.Errorf("dir copy TotalFiles = %d, want 2", res.TotalFiles)
	}
	if readFile(t, filepath.Join(dest, "ai/claude/skills/nested/b.md")) != "skill b\n" {
		t.Error("nested dir file not mirrored")
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
