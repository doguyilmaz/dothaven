package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// maxArchiveEntry bounds a single extracted file. A crafted archive can claim a
// petabyte and fill the disk decompressing it; config files are kilobytes.
const maxArchiveEntry = 256 << 20 // 256 MiB

// IsArchive reports whether path looks like a backup archive rather than a
// backup directory.
func IsArchive(path string) bool {
	return strings.HasSuffix(path, ".tar.gz") || strings.HasSuffix(path, ".tar.gz.age")
}

// IsEncrypted reports whether the archive needs decrypting first.
func IsEncrypted(path string) bool { return strings.HasSuffix(path, ".age") }

// Encrypt wraps an archive with age, which prompts for a passphrase on the
// terminal.
//
// The crypto is delegated on purpose. A backup that travels on a USB stick
// deserves encryption at rest, and the way to get that wrong is to invent a
// password scheme; age already ships with this project's chezmoi story, is
// audited, and gets the passphrase handling right. Nothing here touches a key.
func Encrypt(ctx context.Context, src, dst string) error {
	if _, err := exec.LookPath("age"); err != nil {
		return fmt.Errorf("age is not installed — `brew install age` (it is what encrypts this)")
	}
	cmd := exec.CommandContext(ctx, "age", "--passphrase", "--output", dst, src)
	// age prompts on the terminal; it must reach the user.
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		os.Remove(dst)
		return fmt.Errorf("age failed to encrypt: %w", err)
	}
	return nil
}

// Decrypt unwraps an age-encrypted archive into dst, prompting for the
// passphrase.
func Decrypt(ctx context.Context, src, dst string) error {
	if _, err := exec.LookPath("age"); err != nil {
		return fmt.Errorf("age is not installed — `brew install age` to decrypt this")
	}
	cmd := exec.CommandContext(ctx, "age", "--decrypt", "--output", dst, src)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		os.Remove(dst)
		return fmt.Errorf("age failed to decrypt (wrong passphrase?): %w", err)
	}
	return nil
}

// Extract unpacks a .tar.gz into dst and returns the directory holding the
// backup — archives made here contain a single top-level directory, so that one
// is returned rather than the temporary parent.
//
// Every entry's path is verified to stay inside dst. A tar entry may name
// "../../.ssh/authorized_keys", and an extractor that trusts the name writes it
// there; that is the whole of the zip-slip class of bug, and a tool whose job
// is restoring into a home directory is precisely where it would land.
func Extract(src, dst string) (string, error) {
	f, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
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

		target, err := safeJoin(dst, hdr.Name)
		if err != nil {
			return "", err
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
				return "", fmt.Errorf("%s is larger than %d bytes — refusing to extract", hdr.Name, maxArchiveEntry)
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
// dst and is therefore safe — but it also silently writes a file the archive
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
