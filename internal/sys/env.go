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

func (o *OS) Run(ctx context.Context, args ...string) (string, error) {
	if len(args) == 0 {
		return "", errors.New("empty command")
	}
	ctx, cancel := context.WithTimeout(ctx, CommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return out.String(), nil // tolerate non-zero exit, keep stdout
		}
		return out.String(), err // spawn failure (command not found, ctx cancelled)
	}
	return out.String(), nil
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

// WriteFile writes content to path, creating parent directories as needed. It is
// the single mkdir-p+write primitive shared by backup and restore.
func WriteFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
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
