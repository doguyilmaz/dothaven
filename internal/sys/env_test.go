package sys

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteFilePermsAndContent(t *testing.T) {
	dir := t.TempDir()

	secret := filepath.Join(dir, "nested", "snap.json")
	if err := WriteFileSecure(secret, "topsecret"); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(secret); fi.Mode().Perm() != 0o600 {
		t.Errorf("WriteFileSecure file perm = %o, want 600", fi.Mode().Perm())
	}
	if di, _ := os.Stat(filepath.Dir(secret)); di.Mode().Perm() != 0o700 {
		t.Errorf("WriteFileSecure dir perm = %o, want 700", di.Mode().Perm())
	}
	if b, _ := os.ReadFile(secret); string(b) != "topsecret" {
		t.Errorf("content = %q", b)
	}

	cfg := filepath.Join(dir, "cfg")
	if err := WriteFile(cfg, "y"); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(cfg); fi.Mode().Perm() != 0o644 {
		t.Errorf("WriteFile perm = %o, want 644", fi.Mode().Perm())
	}
}

func TestWriteFileAtomicOverwriteLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	if err := WriteFile(p, "first"); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(p, "second"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "second" {
		t.Errorf("overwrite content = %q, want second", b)
	}
	// No leftover .dothaven-* temp files from the atomic rename.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("expected only the target file, got %d entries", len(entries))
	}
}

func TestRunNilContextNoPanic(t *testing.T) {
	// A nil context must be defaulted (not passed to WithTimeout, which panics).
	// The command not existing is fine — we only assert this doesn't panic.
	if _, err := Real().Run(nil, "dothaven-no-such-command-xyz"); err == nil {
		t.Log("unexpected: bogus command did not error (harmless)")
	}
}

func TestDataDir(t *testing.T) {
	o := &OS{home: "/home/u"}

	t.Setenv("XDG_DATA_HOME", "/xdg")
	if got := o.DataDir(); got != "/xdg/dothaven" {
		t.Errorf("DataDir with XDG = %q, want /xdg/dothaven", got)
	}

	t.Setenv("XDG_DATA_HOME", "")
	if got := o.DataDir(); got != "/home/u/.local/share/dothaven" {
		t.Errorf("DataDir fallback = %q, want ~/.local/share/dothaven", got)
	}
}

func TestResolveOutputDir(t *testing.T) {
	o := &OS{home: "/home/u"}
	t.Setenv("XDG_DATA_HOME", "")

	if got := o.ResolveOutputDir("/explicit"); got != "/explicit" {
		t.Errorf("explicit path = %q, want /explicit", got)
	}
	// In a non-repo cwd it falls back to DataDir, never ~/Downloads.
	orig, _ := os.Getwd()
	defer os.Chdir(orig)
	os.Chdir(t.TempDir())
	cwd, _ := os.Getwd() // resolved (macOS /var -> /private/var)
	if got := o.ResolveOutputDir(""); got != "/home/u/.local/share/dothaven" {
		t.Errorf("non-repo fallback = %q, want ~/.local/share/dothaven", got)
	}
	// Inside a repo it uses ./reports (cwd-local inspection output).
	if err := os.MkdirAll(filepath.Join(cwd, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(cwd, ".git", "HEAD"), []byte("ref: x\n"), 0o644)
	if got := o.ResolveOutputDir(""); got != filepath.Join(cwd, "reports") {
		t.Errorf("repo output = %q, want %s/reports", got, cwd)
	}
}

func TestTimestampUTC(t *testing.T) {
	got := Timestamp(time.Date(2026, 6, 4, 17, 47, 21, 0, time.UTC))
	if got != "20260604174721" {
		t.Errorf("Timestamp = %q, want 20260604174721", got)
	}
}
