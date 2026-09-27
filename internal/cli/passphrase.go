package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// passphraseEnv lets a script supply the archive passphrase. It is how restic
// and borg are automated too; the prompt stays the default because an
// environment variable is visible to every process the shell starts.
const passphraseEnv = "DOTHAVEN_PASSPHRASE"

// minPassphrase is the shortest passphrase accepted for a new archive. The
// archive can hold SSH keys and cloud credentials; scrypt slows guessing but
// cannot rescue a four-letter word.
const minPassphrase = 10

var errNoTerminal = errors.New("no terminal to ask for a passphrase on — set " + passphraseEnv)

// readSecret reads one line from the terminal without echoing it. It opens
// /dev/tty rather than trusting stdin, so it still works when stdin or stdout
// is redirected (`dothaven restore x.age | tee log`).
func readSecret(prompt string) (string, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", errNoTerminal
	}
	defer tty.Close()
	fmt.Fprint(tty, prompt)
	b, err := term.ReadPassword(int(tty.Fd()))
	fmt.Fprintln(tty)
	if err != nil {
		return "", errNoTerminal
	}
	return strings.TrimRight(string(b), "\r\n"), nil
}

// newPassphrase asks for a passphrase for a new archive, twice.
func newPassphrase() (string, error) {
	if p, ok := os.LookupEnv(passphraseEnv); ok {
		if len(p) < minPassphrase {
			return "", fmt.Errorf("%s is shorter than %d characters", passphraseEnv, minPassphrase)
		}
		return p, nil
	}
	fmt.Fprintln(os.Stderr, dim("Choose a passphrase for this backup. You will need it on the new machine,"))
	fmt.Fprintln(os.Stderr, dim("and nothing can recover the backup without it — store it in a password manager."))
	for range 3 {
		p, err := readSecret("  Passphrase: ")
		if err != nil {
			return "", err
		}
		if len(p) < minPassphrase {
			fmt.Fprintf(os.Stderr, "  %s at least %d characters, please — this file can hold your SSH keys.\n", warn("⚠"), minPassphrase)
			continue
		}
		again, err := readSecret("  Again:      ")
		if err != nil {
			return "", err
		}
		if again != p {
			fmt.Fprintf(os.Stderr, "  %s they did not match, try again.\n", warn("⚠"))
			continue
		}
		return p, nil
	}
	return "", errors.New("no passphrase set")
}

// askPassphrase returns a function that asks for an existing archive's
// passphrase — once, when it is first needed, so a plain archive never asks.
func askPassphrase() func() (string, error) {
	return func() (string, error) {
		if p, ok := os.LookupEnv(passphraseEnv); ok {
			return p, nil
		}
		return readSecret("  Passphrase for this backup: ")
	}
}
