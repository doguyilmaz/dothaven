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
// cannot make a short passphrase safe.
const minPassphrase = 10

var errNoTerminal = errors.New("no terminal to ask for a passphrase on. Set " + passphraseEnv)

// secretEnv holds DOTHAVEN_PASSPHRASE and DOTHAVEN_GITHUB_TOKEN once Execute
// has taken them out of the environment. Every process dothaven starts (brew,
// npm and pipx install hooks, chezmoi, git) would otherwise inherit them.
var secretEnv = map[string]string{}

// takeSecretEnv moves the secret variables from the environment into
// secretEnv.
func takeSecretEnv() {
	for _, name := range []string{passphraseEnv, tokenEnv} {
		if v, ok := os.LookupEnv(name); ok {
			secretEnv[name] = v
			_ = os.Unsetenv(name)
		}
	}
}

// lookupSecretEnv reads a secret variable, taken or still in the environment.
func lookupSecretEnv(name string) (string, bool) {
	if v, ok := secretEnv[name]; ok {
		return v, true
	}
	return os.LookupEnv(name)
}

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

// envPassphrase is DOTHAVEN_PASSPHRASE for a new archive, held to the same
// rule as a typed one. Set but empty is an error, not "no passphrase": a
// script whose $PASS expanded to nothing must fail, not upload plaintext.
func envPassphrase() (string, bool, error) {
	p, ok := lookupSecretEnv(passphraseEnv)
	if !ok {
		return "", false, nil
	}
	if len([]rune(p)) < minPassphrase {
		return "", true, fmt.Errorf("%s is set but shorter than %d characters", passphraseEnv, minPassphrase)
	}
	return p, true, nil
}

// newPassphrase asks for a passphrase for a new archive, twice.
func newPassphrase() (string, error) {
	if p, ok, err := envPassphrase(); ok {
		return p, err
	}
	fmt.Fprintln(os.Stderr, dim("Choose a passphrase for this backup. You will need it on the new machine,"))
	fmt.Fprintln(os.Stderr, dim("and nothing can recover the backup without it. Store it in a password manager."))
	for range 3 {
		p, err := readSecret("  Passphrase: ")
		if err != nil {
			return "", err
		}
		if len([]rune(p)) < minPassphrase {
			fmt.Fprintf(os.Stderr, "  %s at least %d characters, please. This file can hold your SSH keys.\n", warn("⚠"), minPassphrase)
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
// passphrase once, when it is first needed, so a plain archive never asks.
func askPassphrase() func() (string, error) {
	return func() (string, error) {
		if p, ok := lookupSecretEnv(passphraseEnv); ok {
			return p, nil
		}
		return readSecret("  Passphrase for this backup: ")
	}
}
