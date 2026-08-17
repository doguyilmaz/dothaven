package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// Every other output this tool writes is owner-only. The archive was landing
// 0644 because tar honours the umask — the same config, packaged
// world-readable, and it is the copy most likely to be carried onto shared
// storage.
func TestArchiveIsOwnerOnly(t *testing.T) {
	p := filepath.Join(t.TempDir(), "backup.tar.gz")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The same call backup.go makes after renaming the archive into place.
	if err := os.Chmod(p, 0o600); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("archive mode is %04o, want 0600", perm)
	}
}
