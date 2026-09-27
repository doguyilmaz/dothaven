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
	if err := s.Set("github", "a\"b"); err == nil {
		t.Error("a value that could break out of the keychain command must be refused")
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
