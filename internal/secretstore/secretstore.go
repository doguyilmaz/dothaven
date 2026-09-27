// Package secretstore keeps small secrets — a GitHub token, a remembered
// backup passphrase — in the operating system's credential store: the macOS
// Keychain, or the Secret Service (GNOME Keyring, KWallet) on Linux. Where
// neither is available it falls back to an owner-only file and says so.
package secretstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const service = "dothaven"

// ErrNotFound means nothing is stored under that name.
var ErrNotFound = errors.New("not stored")

// Store is one backend.
type Store struct {
	kind string // "keychain" | "secret-service" | "file"
	dir  string // file backend only
}

// Open picks the best backend on this machine. configDir is where the file
// fallback lives. DOTHAVEN_SECRET_STORE=file forces the file backend (for a
// headless box without a keyring, and for tests).
func Open(configDir string) *Store {
	s := &Store{kind: "file", dir: filepath.Join(configDir, "credentials")}
	if os.Getenv("DOTHAVEN_SECRET_STORE") == "file" {
		return s
	}
	switch runtime.GOOS {
	case "darwin":
		if _, err := exec.LookPath("security"); err == nil {
			s.kind = "keychain"
		}
	case "linux":
		if _, err := exec.LookPath("secret-tool"); err == nil && os.Getenv("DBUS_SESSION_BUS_ADDRESS") != "" {
			s.kind = "secret-service"
		}
	}
	return s
}

// Name says where secrets are kept, for messages.
func (s *Store) Name() string {
	switch s.kind {
	case "keychain":
		return "the macOS Keychain"
	case "secret-service":
		return "your login keyring"
	}
	return filepath.Join(s.dir, "…") + " (owner-only file; no keyring found)"
}

// run bounds a credential-store call: a keyring that is locked can put up an
// unlock prompt, and nothing here should wait on one forever.
func run(stdin string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 2 * time.Second
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out, err := cmd.Output()
	return strings.TrimRight(string(out), "\r\n"), err
}

// Get returns the secret stored under account.
func (s *Store) Get(account string) (string, error) {
	switch s.kind {
	case "keychain":
		v, err := run("", "security", "find-generic-password", "-s", service, "-a", account, "-w")
		if err != nil || v == "" {
			return "", ErrNotFound
		}
		return v, nil
	case "secret-service":
		v, err := run("", "secret-tool", "lookup", "service", service, "account", account)
		if err != nil || v == "" {
			return "", ErrNotFound
		}
		return v, nil
	}
	b, err := os.ReadFile(filepath.Join(s.dir, account))
	if err != nil {
		return "", ErrNotFound
	}
	return strings.TrimSpace(string(b)), nil
}

// Set stores secret under account, replacing any previous value. The secret
// never appears in a command line (which other processes can read): the
// Keychain gets it through `security -i` on stdin, the keyring through
// secret-tool's stdin.
func (s *Store) Set(account, secret string) error {
	if strings.ContainsAny(secret, "\"\n\r") || strings.ContainsAny(account, "\"\n\r ") {
		return fmt.Errorf("cannot store a value containing quotes or newlines")
	}
	switch s.kind {
	case "keychain":
		cmd := fmt.Sprintf("add-generic-password -U -s %s -a \"%s\" -l \"dothaven %s\" -w \"%s\"\n", service, account, account, secret)
		_, err := run(cmd, "security", "-i")
		return err
	case "secret-service":
		_, err := run(secret, "secret-tool", "store", "--label=dothaven "+account, "service", service, "account", account)
		return err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(s.dir, 0o700); err != nil {
		return err
	}
	tmp := filepath.Join(s.dir, "."+account+".tmp")
	if err := os.WriteFile(tmp, []byte(secret+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.dir, account))
}

// Delete removes account's secret. Deleting something absent is not an error.
func (s *Store) Delete(account string) error {
	switch s.kind {
	case "keychain":
		_, _ = run("", "security", "delete-generic-password", "-s", service, "-a", account)
		return nil
	case "secret-service":
		_, _ = run("", "secret-tool", "clear", "service", service, "account", account)
		return nil
	}
	err := os.Remove(filepath.Join(s.dir, account))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
