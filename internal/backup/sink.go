package backup

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"filippo.io/age"
	"filippo.io/age/armor"

	"github.com/doguyilmaz/dothaven/internal/scan"
	"github.com/doguyilmaz/dothaven/internal/sys"
)

// Sink is where a backup's files go. One interface for a directory and an
// archive, so the two cannot drift: a folder backup and an encrypted one are
// the same walk, the same gate and the same manifest.
type Sink interface {
	Add(dest string, data []byte, exec bool) error
}

// ClassifyingSink is a Sink that is told, with each file, whether it is
// sensitive: a credential entry, a file with a secret in it, or anything
// inside a credential root.
type ClassifyingSink interface {
	Sink
	AddClassified(dest string, data []byte, exec, sensitive bool) error
}

// SplitSink sends sensitive files to Secret (an encrypted archive) and the rest
// to Plain — readable, diffable config beside an encrypted bundle of whatever
// must not be read. Files added without a classification (the inventory, the
// manifest) are judged by a scan.
type SplitSink struct {
	Plain, Secret Sink
	Secrets       int
}

func (s *SplitSink) Add(dest string, data []byte, exec bool) error {
	return s.AddClassified(dest, data, exec, s.sensitive(dest, data))
}

// sensitive is how an unclassified file is placed: anything the scanner would
// not include verbatim goes to the encrypted part.
func (s *SplitSink) sensitive(dest string, data []byte) bool {
	return scan.ScanContentFull(dest, string(data)).Action != scan.Include
}

func (s *SplitSink) AddClassified(dest string, data []byte, exec, sensitive bool) error {
	if sensitive {
		s.Secrets++
		return s.Secret.Add(dest, data, exec)
	}
	return s.Plain.Add(dest, data, exec)
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

// WriteArchive builds a plain .tar.gz at dst by handing fill a Sink to add
// files to. WriteEncryptedArchive is the encrypted one; they are separate so
// that no value of a variable can turn "encrypted" into "plain" — an empty
// passphrase is an error there, never a request for no encryption.
func WriteArchive(dst, root string, fill func(Sink) error) error {
	return writeArchive(dst, root, "", false, fill)
}

// ErrNoPassphrase is returned when an encrypted archive is asked for without a
// passphrase.
var ErrNoPassphrase = errors.New("an encrypted archive needs a passphrase")

// WriteEncryptedArchive builds an age-encrypted .tar.gz at dst.
//
// Everything streams: files go through tar and gzip and, when encrypting,
// straight into age, so an encrypted backup never exists in plaintext on disk,
// not even as a temporary file. It is written to dst+".partial" and renamed only
// once complete, owner-only from the first byte.
//
// Encryption uses the age library, not the age binary. The format is age's
// own, so `age -d` opens these files, but the machine you restore on does not
// need age installed — which, on a freshly wiped laptop, it will not be.
func WriteEncryptedArchive(dst, root, passphrase string, fill func(Sink) error) error {
	if passphrase == "" {
		return ErrNoPassphrase
	}
	return writeArchive(dst, root, passphrase, true, fill)
}

func writeArchive(dst, root, passphrase string, encrypt bool, fill func(Sink) error) (err error) {
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
	if encrypt {
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

// AgeHeader returns the start of an age file: its header, which holds the
// passphrase-wrapped file key, and the payload nonce. That is enough to check
// a passphrase against the file without the rest of it, and it reveals
// nothing the file itself does not.
func AgeHeader(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, 64<<10)
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, err
	}
	buf = buf[:n]
	if !bytes.HasPrefix(buf, []byte(ageMagic)) {
		return nil, errors.New("not an age file")
	}
	// The header ends with the MAC line, "--- <mac>\n"; the 16-byte payload
	// nonce follows.
	i := bytes.Index(buf, []byte("\n--- "))
	if i < 0 {
		return nil, errors.New("age header not found")
	}
	end := bytes.IndexByte(buf[i+1:], '\n')
	if end < 0 {
		return nil, errors.New("age header not found")
	}
	end += i + 2
	if len(buf) < end+16 {
		return nil, errors.New("age file too short")
	}
	return buf[:end+16], nil
}

// HeaderOpens reports whether passphrase unlocks the age header hdr, as
// returned by AgeHeader.
func HeaderOpens(hdr []byte, passphrase string) bool {
	id, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return false
	}
	_, err = age.Decrypt(bytes.NewReader(hdr), id)
	return err == nil
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
	return ExtractArchiveOnly(src, dst, passphrase, nil)
}

// ExtractArchiveOnly is ExtractArchive writing only the entries keep accepts.
func ExtractArchiveOnly(src, dst string, passphrase func() (string, error), keep func(string) bool) (string, error) {
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
	return ExtractReaderOnly(r, dst, keep)
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

// DigestSink passes files through to an inner sink while fingerprinting what
// went in: path, content hash and exec bit, nothing time-dependent. Two
// backups of an unchanged machine have the same Sum even when their encrypted
// archives share not a byte — which is how a push knows it has nothing to do.
type DigestSink struct {
	Inner   Sink
	Exclude map[string]bool // dests left out of the fingerprint (the MANIFEST's timestamp)
	lines   []string
}

// note records one file. class is where a classifying sink put it ("" when
// nothing classified it): a file that moves between the readable and the
// encrypted part has changed, even if its bytes have not.
func (d *DigestSink) note(dest string, data []byte, exec bool, class string) {
	if d.Exclude[dest] {
		return
	}
	sum := sha256.Sum256(data)
	d.lines = append(d.lines, fmt.Sprintf("%s\x00%x\x00%v\x00%s", dest, sum, exec, class))
}

func (d *DigestSink) Add(dest string, data []byte, exec bool) error {
	if cs, ok := d.Inner.(*SplitSink); ok {
		// Let the split sink decide, and record what it decided.
		return d.AddClassified(dest, data, exec, cs.sensitive(dest, data))
	}
	d.note(dest, data, exec, "")
	return d.Inner.Add(dest, data, exec)
}

// AddClassified keeps the inner sink's classification: a digest wrapped
// around a SplitSink must not turn "this is a credential" into "scan it and
// see".
func (d *DigestSink) AddClassified(dest string, data []byte, exec, sensitive bool) error {
	class := "plain"
	if sensitive {
		class = "secret"
	}
	d.note(dest, data, exec, class)
	if cs, ok := d.Inner.(ClassifyingSink); ok {
		return cs.AddClassified(dest, data, exec, sensitive)
	}
	return d.Inner.Add(dest, data, exec)
}

// Sum is the fingerprint of everything added so far.
func (d *DigestSink) Sum() string {
	lines := append([]string(nil), d.lines...)
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}
