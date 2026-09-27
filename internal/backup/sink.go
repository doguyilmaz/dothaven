package backup

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"filippo.io/age"
	"filippo.io/age/armor"

	"github.com/doguyilmaz/dothaven/internal/sys"
)

// Sink is where a backup's files go. One interface for a directory and an
// archive, so the two cannot drift: a folder backup and an encrypted one are
// the same walk, the same gate and the same manifest.
type Sink interface {
	Add(dest string, data []byte, exec bool) error
}

// DirSink writes each file owner-only under Root. Executable files keep their
// bit (0700) — a restored git hook that is not executable is silently ignored
// by git, which is the worst way to lose one.
type DirSink struct{ Root string }

func (d DirSink) Add(dest string, data []byte, exec bool) error {
	perm := os.FileMode(0o600)
	if exec {
		perm = 0o700
	}
	return sys.WriteBytesAs(filepath.Join(d.Root, filepath.FromSlash(dest)), data, perm)
}

// tarSink streams files into a tar under a single top-level directory, which
// is what Extract expects to find.
type tarSink struct {
	tw   *tar.Writer
	root string
	now  time.Time
}

func (t *tarSink) Add(dest string, data []byte, exec bool) error {
	mode := int64(0o600)
	if exec {
		mode = 0o700
	}
	hdr := &tar.Header{
		Name:     path.Join(t.root, dest),
		Mode:     mode,
		Size:     int64(len(data)),
		ModTime:  t.now,
		Typeflag: tar.TypeReg,
		Format:   tar.FormatPAX,
	}
	if err := t.tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err := t.tw.Write(data)
	return err
}

// ErrNothingToWrite aborts an archive that would hold nothing, so no empty file
// is left behind looking like a backup.
var ErrNothingToWrite = errors.New("nothing to write")

// WriteArchive builds a .tar.gz at dst — age-encrypted with passphrase when one
// is given — by handing fill a Sink to add files to.
//
// Everything streams: files go through tar and gzip and, when encrypting,
// straight into age, so an encrypted backup never exists in plaintext on disk,
// not even as a temporary file. It is written to dst+".partial" and renamed only
// once complete, owner-only from the first byte.
//
// Encryption uses the age library, not the age binary. The format is age's
// own, so `age -d` opens these files, but the machine you restore on does not
// need age installed — which, on a freshly wiped laptop, it will not be.
func WriteArchive(dst, root, passphrase string, fill func(Sink) error) (err error) {
	tmp := dst + ".partial"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(tmp)
		}
	}()

	var w io.Writer = f
	var enc io.WriteCloser
	if passphrase != "" {
		r, rerr := age.NewScryptRecipient(passphrase)
		if rerr != nil {
			return rerr
		}
		if enc, err = age.Encrypt(f, r); err != nil {
			return err
		}
		w = enc
	}
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	if err = fill(&tarSink{tw: tw, root: root, now: time.Now()}); err != nil {
		return err
	}
	if err = tw.Close(); err != nil {
		return err
	}
	if err = gz.Close(); err != nil {
		return err
	}
	if enc != nil {
		if err = enc.Close(); err != nil {
			return err
		}
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// ErrWrongPassphrase is returned when an encrypted archive will not open with
// the passphrase given.
var ErrWrongPassphrase = errors.New("wrong passphrase")

const ageMagic = "age-encryption.org/"

// Format is what a backup path turned out to be.
type Format int

const (
	FormatDir Format = iota
	FormatTarGz
	FormatAge
	FormatUnknown
)

// Detect reports what path holds, by content rather than by name: a backup
// renamed on the way across (or saved by a browser as "backup (1).age") must
// still open.
func Detect(path string) Format {
	fi, err := os.Stat(path)
	if err != nil {
		return FormatUnknown
	}
	if fi.IsDir() {
		return FormatDir
	}
	f, err := os.Open(path)
	if err != nil {
		return FormatUnknown
	}
	defer f.Close()
	head := make([]byte, 64)
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	switch {
	case bytes.HasPrefix(head, []byte(ageMagic)), bytes.HasPrefix(head, []byte(armor.Header)):
		return FormatAge
	case len(head) >= 2 && head[0] == 0x1f && head[1] == 0x8b:
		return FormatTarGz
	}
	return FormatUnknown
}

// ExtractArchive unpacks a .tar.gz or an age-encrypted one into dst and returns
// the backup directory inside it. passphrase is asked only when the archive is
// encrypted.
func ExtractArchive(src, dst string, passphrase func() (string, error)) (string, error) {
	f, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var r io.Reader = bufio.NewReader(f)
	if Detect(src) == FormatAge {
		pass, err := passphrase()
		if err != nil {
			return "", err
		}
		id, err := age.NewScryptIdentity(pass)
		if err != nil {
			return "", err
		}
		br := r.(*bufio.Reader)
		if peek, _ := br.Peek(len(armor.Header)); string(peek) == armor.Header {
			r = armor.NewReader(br)
		}
		dec, err := age.Decrypt(r, id)
		if err != nil {
			var nomatch *age.NoIdentityMatchError
			if errors.As(err, &nomatch) || strings.Contains(err.Error(), "incorrect") {
				return "", ErrWrongPassphrase
			}
			return "", fmt.Errorf("cannot decrypt: %w", err)
		}
		r = dec
	}
	return ExtractReader(r, dst)
}

// Verify reads an archive end to end — decrypting, decompressing and walking
// every entry — and returns how many files it holds. Nothing is written: this
// is how a backup proves it opens before the machine it came from is wiped,
// without putting its contents on disk a second time.
func Verify(src string, passphrase func() (string, error)) (int, error) {
	f, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	var r io.Reader = bufio.NewReader(f)
	if Detect(src) == FormatAge {
		pass, err := passphrase()
		if err != nil {
			return 0, err
		}
		id, err := age.NewScryptIdentity(pass)
		if err != nil {
			return 0, err
		}
		dec, err := age.Decrypt(r, id)
		if err != nil {
			return 0, ErrWrongPassphrase
		}
		r = dec
	}
	gz, err := gzip.NewReader(r)
	if err != nil {
		return 0, err
	}
	tr := tar.NewReader(gz)
	n := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return n, nil
		}
		if err != nil {
			return n, err
		}
		// Reading the body is what checks it: age authenticates each chunk
		// as it is read, so a corrupt byte anywhere fails here.
		if _, err := io.Copy(io.Discard, tr); err != nil {
			return n, err
		}
		if hdr.Typeflag == tar.TypeReg {
			n++
		}
	}
}
