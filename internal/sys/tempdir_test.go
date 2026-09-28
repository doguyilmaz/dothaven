package sys

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestPrivateTempDirIsTrackedAndSwept(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	dir, done, err := PrivateTempDir("restore")
	if err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o700 {
		t.Errorf("perm = %o", fi.Mode().Perm())
	}
	os.WriteFile(filepath.Join(dir, "secret"), []byte("x"), 0o600)
	RemoveTempDirs() // the forced-exit path
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("RemoveTempDirs left the folder")
	}
	done() // removing twice is harmless

	// A folder from a process that is gone is swept — only one with our
	// exact name, our marker, and old enough. Look-alikes are someone's work.
	old := time.Now().Add(-2 * time.Hour)
	mk := func(name string, marker bool, at time.Time) string {
		d := filepath.Join(os.TempDir(), name)
		os.MkdirAll(d, 0o700)
		if marker {
			m := filepath.Join(d, tempMarker)
			os.WriteFile(m, nil, 0o600)
			os.Chtimes(m, at, at)
		}
		return d
	}
	dead := mk("dothaven-restore-999999999-123", true, old)
	keep := []string{
		mk("dothaven-restore-"+strconv.Itoa(os.Getpid())+"-123", true, old), // this process
		mk("dothaven-restore-999999998-123", true, time.Now()),              // too recent
		mk("dothaven-restore-999999997-123", false, old),                    // no marker
		mk("dothaven-pr-4242-fix", true, old),                               // a user's folder
		mk("dothaven-docs-31337-draft", true, old),
		mk("not-ours-999999999-123", true, old),
	}
	if n := SweepTempDirs(); n != 1 {
		t.Errorf("swept %d, want 1", n)
	}
	if _, err := os.Stat(dead); !os.IsNotExist(err) {
		t.Error("a dead process's folder was left")
	}
	for _, d := range keep {
		if _, err := os.Stat(d); err != nil {
			t.Errorf("%s was removed", d)
		}
	}
}
