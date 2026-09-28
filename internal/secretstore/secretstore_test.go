package secretstore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileBackendRoundTrip(t *testing.T) {
	t.Setenv("DOTHAVEN_SECRET_STORE", "file")
	dir := t.TempDir()
	s := Open(dir)
	if _, err := s.Get("github"); err != ErrNotFound {
		t.Fatalf("empty store: %v", err)
	}
	if err := s.Set("github", "gho_abc123"); err != nil {
		t.Fatal(err)
	}
	if v, err := s.Get("github"); err != nil || v != "gho_abc123" {
		t.Fatalf("Get = %q, %v", v, err)
	}
	fi, _ := os.Stat(filepath.Join(dir, "credentials", "github"))
	di, _ := os.Stat(filepath.Join(dir, "credentials"))
	if fi.Mode().Perm() != 0o600 || di.Mode().Perm() != 0o700 {
		t.Errorf("modes: file %o dir %o", fi.Mode().Perm(), di.Mode().Perm())
	}
	// Anything a passphrase can contain comes back exactly: quotes and
	// backslashes (which the Keychain's command parser would otherwise see),
	// spaces at the ends, non-ASCII letters, even a newline.
	for _, v := range []string{`a"b\c`, "  padded passphrase ", "şifre-ğüç-ıİ-çok-gizli", "two\nlines"} {
		if err := s.Set("backup-passphrase", v); err != nil {
			t.Fatalf("Set(%q): %v", v, err)
		}
		if got, err := s.Get("backup-passphrase"); err != nil || got != v {
			t.Errorf("Get = %q, %v; want %q", got, err, v)
		}
	}
	// What is on disk is encoded, so no backend ever parses the secret.
	if b, _ := os.ReadFile(filepath.Join(dir, "credentials", "backup-passphrase")); string(b) == "two\nlines\n" {
		t.Error("stored unencoded")
	}
	// A value stored by an older version (unencoded) still reads.
	os.WriteFile(filepath.Join(dir, "credentials", "old"), []byte("gho_legacy\n"), 0o600)
	if got, _ := s.Get("old"); got != "gho_legacy" {
		t.Errorf("legacy value = %q", got)
	}
	if err := s.Set("bad account", "x"); err == nil {
		t.Error("an account name that could break the keychain command must be refused")
	}
	if err := s.Delete("github"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("github"); err != ErrNotFound {
		t.Error("still there after Delete")
	}
	if err := s.Delete("github"); err != nil {
		t.Error("deleting twice should not fail")
	}
}

func TestGetExactTellsLegacyValuesApart(t *testing.T) {
	t.Setenv("DOTHAVEN_SECRET_STORE", "file")
	dir := t.TempDir()
	s := Open(dir)
	if err := s.Set("p", "şifre çok gizli"); err != nil {
		t.Fatal(err)
	}
	if v, exact, err := s.GetExact("p"); err != nil || !exact || v != "şifre çok gizli" {
		t.Errorf("new value = %q exact=%v %v", v, exact, err)
	}
	os.WriteFile(filepath.Join(dir, "credentials", "old"), []byte("c59f6966726520\n"), 0o600)
	if _, exact, _ := s.GetExact("old"); exact {
		t.Error("a raw value from an older version reported as exact")
	}
}
