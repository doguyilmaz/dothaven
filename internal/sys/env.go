// Package sys is the injectable seam for side effects: running commands,
// reading files/dirs, env vars, and path resolution. Collectors and commands
// take an Env so they can be tested with a fake.
package sys

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type Env interface {
	// Run executes a command and returns its stdout. A non-zero exit is tolerated
	// (e.g. `npm ls` exits 1 on peer warnings) — only a spawn failure is an error.
	Run(ctx context.Context, args ...string) (string, error)
	ReadFile(path string) ([]byte, error)
	// ListDir returns the names of a directory's entries (non-recursive).
	ListDir(path string) ([]string, error)
	Exists(path string) bool
	Getenv(key string) string
	Home() string
}

// OS is the real, filesystem-backed Env.
type OS struct{ home string }

func Real() *OS {
	h, _ := os.UserHomeDir()
	return &OS{home: h}
}

// CommandTimeout bounds a single external command so one hung tool (an
// unreachable registry, a wedged daemon, a first-run license prompt) can't
// block the whole run. CommandContext SIGKILLs the child when it fires.
const CommandTimeout = 30 * time.Second

// maxCmdOutput caps captured stdout so a misbehaving tool can't stream
// unbounded data into memory. Tool output is line-oriented and parsed; 16 MiB
// is far more than any real listing.
const maxCmdOutput = 16 << 20

// capBuffer collects up to cap bytes and silently drops the rest, reporting
// every write as fully consumed so the child never sees a short-write/EPIPE.
type capBuffer struct {
	buf bytes.Buffer
	cap int
}

func (c *capBuffer) Write(p []byte) (int, error) {
	if room := c.cap - c.buf.Len(); room > 0 {
		if len(p) > room {
			c.buf.Write(p[:room])
		} else {
			c.buf.Write(p)
		}
	}
	return len(p), nil
}

func (o *OS) Run(ctx context.Context, args ...string) (string, error) {
	if len(args) == 0 {
		return "", errors.New("empty command")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, CommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	out := &capBuffer{cap: maxCmdOutput}
	cmd.Stdout = out
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return out.buf.String(), nil // tolerate non-zero exit, keep stdout
		}
		return out.buf.String(), err // spawn failure (command not found, ctx cancelled)
	}
	return out.buf.String(), nil
}

func (o *OS) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

func (o *OS) ListDir(path string) ([]string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return names, nil
}

func (o *OS) Exists(path string) bool  { _, err := os.Stat(path); return err == nil }
func (o *OS) Getenv(key string) string { return os.Getenv(key) }
func (o *OS) Home() string             { return o.home }

// Timestamp formats a time as YYYYMMDDHHMMSS (UTC) for output filenames.
func Timestamp(t time.Time) string { return t.UTC().Format("20060102150405") }

// WriteFile atomically writes content to path (0644), creating parent dirs. Used
// for restoring ordinary configs.
func WriteFile(path, content string) error {
	return writeFile(path, content, 0o755, 0o644)
}

// WriteFileSecure atomically writes content owner-only (0600 file, 0700 dirs).
// Used for any output that can hold secrets — backups, snapshots, security
// reports, pre-restore snapshots — so it is never world-readable.
func WriteFileSecure(path, content string) error {
	return writeFile(path, content, 0o700, 0o600)
}

// writeFile writes to a temp file in the destination dir and renames it into
// place, so an interrupted write can never leave a half-written (or empty)
// target — the rename is atomic on the same filesystem.
func writeFile(path, content string, dirPerm, filePerm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".dothaven-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func(e error) error { tmp.Close(); os.Remove(tmpName); return e }
	if _, err := tmp.WriteString(content); err != nil {
		return cleanup(err)
	}
	if err := tmp.Chmod(filePerm); err != nil {
		return cleanup(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// ResolveOutputDir decides where reports/backups land: an explicit path wins;
// inside a git repo → <cwd>/reports; otherwise → ~/Downloads.
func (o *OS) ResolveOutputDir(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if cwd, err := os.Getwd(); err == nil {
		if o.Exists(filepath.Join(cwd, ".git/HEAD")) {
			return filepath.Join(cwd, "reports")
		}
	}
	return filepath.Join(o.home, "Downloads")
}
