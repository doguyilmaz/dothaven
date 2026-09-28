package sys

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
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

	// A folder from a process that is gone is swept; ours and a live one's are not.
	dead := filepath.Join(os.TempDir(), "dothaven-restore-999999999-abc")
	mine := filepath.Join(os.TempDir(), "dothaven-restore-"+itoa(os.Getpid())+"-abc")
	other := filepath.Join(os.TempDir(), "not-ours-999999999-abc")
	for _, d := range []string{dead, mine, other} {
		os.MkdirAll(d, 0o700)
	}
	if n := SweepTempDirs(); n != 1 {
		t.Errorf("swept %d, want 1", n)
	}
	if _, err := os.Stat(dead); !os.IsNotExist(err) {
		t.Error("a dead process's folder was left")
	}
	for _, d := range []string{mine, other} {
		if _, err := os.Stat(d); err != nil {
			t.Errorf("%s was removed", d)
		}
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
