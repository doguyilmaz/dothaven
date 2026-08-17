package backup

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func writeTarGz(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()
	for name, body := range entries {
		if err := tw.WriteHeader(&tar.Header{
			Name: name, Mode: 0o600, Size: int64(len(body)), Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestExtractRoundTrip(t *testing.T) {
	d := t.TempDir()
	src := filepath.Join(d, "b.tar.gz")
	writeTarGz(t, src, map[string]string{
		"backup-mac-1/shell/.zshrc": "export PATH=/usr/bin\n",
		"backup-mac-1/MANIFEST.txt": "one file\n",
	})

	dst := t.TempDir()
	root, err := Extract(src, dst)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(root) != "backup-mac-1" {
		t.Errorf("root = %q, want the backup dir inside the archive", root)
	}
	got, err := os.ReadFile(filepath.Join(root, "shell", ".zshrc"))
	if err != nil || string(got) != "export PATH=/usr/bin\n" {
		t.Errorf("content did not survive: %q %v", got, err)
	}
}

// A tar entry may name its way out of the extraction directory. A tool that
// restores into a home directory is exactly where that lands, so it is refused
// rather than sanitised silently.
func TestExtractRefusesPathTraversal(t *testing.T) {
	d := t.TempDir()
	src := filepath.Join(d, "evil.tar.gz")
	writeTarGz(t, src, map[string]string{
		"../../../../../../tmp/dothaven-escaped": "pwned\n",
	})

	dst := t.TempDir()
	if _, err := Extract(src, dst); err == nil {
		t.Fatal("an entry escaping the extraction directory must be an error")
	}
	if _, err := os.Stat("/tmp/dothaven-escaped"); err == nil {
		os.Remove("/tmp/dothaven-escaped")
		t.Fatal("the escaping entry was actually written")
	}
}

// Symlinks are another way out of the directory, and a config backup has no
// use for one.
func TestExtractSkipsSymlinks(t *testing.T) {
	d := t.TempDir()
	src := filepath.Join(d, "link.tar.gz")
	f, _ := os.Create(src)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "b/evil", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd", Mode: 0o777})
	tw.Close()
	gz.Close()
	f.Close()

	dst := t.TempDir()
	if _, err := Extract(src, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(dst, "b", "evil")); err == nil {
		t.Error("symlink should not have been written")
	}
}

func TestIsArchiveAndIsEncrypted(t *testing.T) {
	cases := map[string][2]bool{ // path -> {IsArchive, IsEncrypted}
		"/x/backup-mac.tar.gz":     {true, false},
		"/x/backup-mac.tar.gz.age": {true, true},
		"/x/backup-mac":            {false, false},
	}
	for path, want := range cases {
		if got := IsArchive(path); got != want[0] {
			t.Errorf("IsArchive(%q) = %v, want %v", path, got, want[0])
		}
		if got := IsEncrypted(path); got != want[1] {
			t.Errorf("IsEncrypted(%q) = %v, want %v", path, got, want[1])
		}
	}
}

// macOS tar writes an AppleDouble sidecar ("._name") per file, and it lands
// first in the archive. Reading the root from entry order picked that up as the
// backup directory, so a real archive extracted to a root containing nothing.
func TestExtractIgnoresAppleDoubleSidecars(t *testing.T) {
	d := t.TempDir()
	src := filepath.Join(d, "mac.tar.gz")
	writeTarGz(t, src, map[string]string{
		"._backup-mac-1":            "\x00\x05\x16\x07xattrs",
		"backup-mac-1/shell/.zshrc": "export PATH=/usr/bin\n",
		"backup-mac-1/._MANIFEST":   "more xattrs",
		"backup-mac-1/.DS_Store":    "finder junk",
	})

	dst := t.TempDir()
	root, err := Extract(src, dst)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(root) != "backup-mac-1" {
		t.Fatalf("root = %q, want backup-mac-1", filepath.Base(root))
	}
	if _, err := os.Stat(filepath.Join(root, "shell", ".zshrc")); err != nil {
		t.Errorf("the real file did not survive: %v", err)
	}
	for _, junk := range []string{"._MANIFEST", ".DS_Store"} {
		if _, err := os.Stat(filepath.Join(root, junk)); err == nil {
			t.Errorf("%s should not be extracted into a home directory", junk)
		}
	}
}
