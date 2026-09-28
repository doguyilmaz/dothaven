package backup

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"github.com/doguyilmaz/dothaven/internal/sys"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// maxArchiveEntry bounds a single extracted file. A crafted archive can claim a
// petabyte and fill the disk decompressing it; config files are kilobytes.
const maxArchiveEntry = 256 << 20 // 256 MiB

// IsArchive reports whether path is a backup archive (plain or encrypted)
// rather than a backup directory. Judged by content, see Detect.
func IsArchive(path string) bool {
	f := Detect(path)
	return f == FormatTarGz || f == FormatAge
}

// Extract unpacks a .tar.gz into dst and returns the directory holding the
// backup. Archives made here contain a single top-level directory, so that one
// is returned rather than the temporary parent.
//
// Every entry's path is verified to stay inside dst. A tar entry may name
// "../../.ssh/authorized_keys", and an extractor that trusts the name writes it
// there. That is the zip-slip class of bug, and a tool that restores into a
// home directory is where it does the most damage.
func Extract(src, dst string) (string, error) {
	f, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return ExtractReader(f, dst)
}

// ExtractReader is Extract over any gzip stream: a file, or the plaintext
// coming out of age.
func ExtractReader(r io.Reader, dst string) (string, error) {
	return ExtractReaderOnly(r, dst, nil)
}

// Only returns a filter for ExtractReaderOnly that keeps the named top-level
// folders of a backup (inventory, macos-defaults), with or without the
// backup's own root folder in front.
func Only(dirs ...string) func(name string) bool {
	return func(name string) bool {
		name = strings.TrimPrefix(filepath.ToSlash(name), "./")
		_, rest, _ := strings.Cut(name, "/")
		for _, d := range dirs {
			for _, n := range []string{name, rest} {
				if n == d || strings.HasPrefix(n, d+"/") {
					return true
				}
			}
		}
		return false
	}
}

// ExtractReaderOnly is ExtractReader writing only the entries keep accepts
// (nil keeps all). A command that needs the inventory reads the whole stream
// but puts only the inventory on disk, not every decrypted key with it.
func ExtractReaderOnly(r io.Reader, dst string, keep func(name string) bool) (string, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return "", fmt.Errorf("not a gzip archive: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}

		// macOS tar writes an AppleDouble sidecar ("._name") per file to carry
		// extended attributes. They are not config, restoring them would litter
		// the home directory, and the first one in the archive was being read
		// as the backup's root directory.
		if strings.HasPrefix(filepath.Base(hdr.Name), "._") || filepath.Base(hdr.Name) == ".DS_Store" {
			continue
		}

		if sys.Aborting() {
			return "", errors.New("interrupted")
		}
		target, err := safeJoin(dst, hdr.Name)
		if err != nil {
			return "", err
		}
		if keep != nil && hdr.Typeflag != tar.TypeDir && !keep(hdr.Name) {
			continue
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return "", err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return "", err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return "", err
			}
			n, err := io.Copy(out, io.LimitReader(tr, maxArchiveEntry+1))
			out.Close()
			if err != nil {
				return "", err
			}
			if n > maxArchiveEntry {
				return "", fmt.Errorf("%s is larger than %d bytes, refusing to extract", hdr.Name, maxArchiveEntry)
			}
		default:
			// Symlinks and device nodes are not written: a link is another way
			// to escape dst, and a backup of config files has no use for one.
			continue
		}
	}
	// The root is found after extracting rather than from the first entry:
	// entry order is a property of whichever tar wrote the archive, and on
	// macOS the first one is metadata. One directory and nothing else means
	// that directory is the backup.
	return singleChildDir(dst), nil
}

// singleChildDir returns dir's only subdirectory when that is all it holds,
// else dir itself.
func singleChildDir(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return dir
	}
	found := ""
	for _, e := range entries {
		if !e.IsDir() {
			return dir // a loose file at the top means this is already the root
		}
		if found != "" {
			return dir // more than one
		}
		found = e.Name()
	}
	if found == "" {
		return dir
	}
	return filepath.Join(dir, found)
}

// safeJoin resolves name under dst and refuses anything that escapes it.
//
// The escape is detected on the name as written, not after cleaning it.
// Cleaning "../../etc/passwd" turns it into "etc/passwd", which lands inside
// dst and is therefore safe, but it also silently writes a file the archive
// did not name, in a place that looks legitimate. An entry that tries to leave
// the directory is malicious or corrupt either way, and both are worth
// stopping rather than quietly rewriting.
func safeJoin(dst, name string) (string, error) {
	if filepath.IsAbs(name) {
		return "", fmt.Errorf("archive entry %q is an absolute path", name)
	}
	for _, part := range strings.Split(filepath.ToSlash(name), "/") {
		if part == ".." {
			return "", fmt.Errorf("archive entry %q escapes the extraction directory", name)
		}
	}
	target := filepath.Join(dst, filepath.Clean(name))
	// Belt and braces: whatever the name looked like, the result must be inside.
	if target != dst && !strings.HasPrefix(target, dst+string(os.PathSeparator)) {
		return "", fmt.Errorf("archive entry %q escapes the extraction directory", name)
	}
	return target, nil
}
