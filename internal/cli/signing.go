package cli

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/doguyilmaz/dothaven/internal/github"
	"github.com/doguyilmaz/dothaven/internal/sys"
)

// gitSigning is how the user's git signs commits, which is how dothaven signs
// its pushes: GitHub marks a commit Verified when its signature checks out
// against a signing key on the committer's account.
type gitSigning struct {
	Format  string // "ssh" or "openpgp"
	Key     string // user.signingkey as written
	Program string // ssh-keygen or gpg, or what gpg.*program names
}

// loadGitSigning reads the user's global git config. It reports false when
// git does not sign commits here (commit.gpgsign off), or signs in a way
// dothaven cannot drive (x509, or SSH with no user.signingkey).
func loadGitSigning(ctx context.Context) (gitSigning, bool) {
	if _, err := exec.LookPath("git"); err != nil {
		return gitSigning{}, false
	}
	// From "/", so a repository the user happens to be standing in does not
	// decide how their backups are signed; system and global config (and
	// their includes) still apply.
	get := func(key string, extra ...string) string {
		out, err := runShell(ctx, "git", append(append([]string{"-C", "/", "config"}, extra...), "--get", key)...)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(out)
	}
	if get("commit.gpgsign", "--type=bool") != "true" {
		return gitSigning{}, false
	}
	g := gitSigning{Format: cmp.Or(get("gpg.format"), "openpgp"), Key: get("user.signingkey")}
	switch g.Format {
	case "ssh":
		if g.Key == "" {
			return gitSigning{}, false
		}
		g.Program = cmp.Or(get("gpg.ssh.program"), "ssh-keygen")
	case "openpgp":
		g.Program = cmp.Or(get("gpg.openpgp.program"), get("gpg.program"), "gpg")
	default:
		return gitSigning{}, false
	}
	return g, true
}

// describe names the key for status lines, never anything secret: a key
// path, a GPG key id, or the type of a literal public key.
func (g gitSigning) describe() string {
	if g.Format == "openpgp" {
		if g.Key == "" {
			return "your default GPG key"
		}
		return "GPG key " + g.Key
	}
	if lit, ok := literalSSHKey(g.Key); ok {
		kind, _, _ := strings.Cut(lit, " ")
		return "SSH key (" + kind + ")"
	}
	return "SSH key " + g.Key
}

// literalSSHKey recognises a public key written into user.signingkey itself,
// which git accepts with a "key::" prefix or, for compatibility, bare.
func literalSSHKey(k string) (string, bool) {
	if v, ok := strings.CutPrefix(k, "key::"); ok {
		return v, true
	}
	for _, p := range []string{"ssh-", "ecdsa-", "sk-"} {
		if strings.HasPrefix(k, p) {
			return k, true
		}
	}
	return "", false
}

// signTimeout is long enough to type a passphrase or touch a security key.
const signTimeout = 2 * time.Minute

// signer signs commit payloads as git does: `ssh-keygen -Y sign -n git` or
// `gpg -bsau`, the payload on stdin and the armoured signature on stdout. A
// passphrase prompt goes to the terminal, as it would for git.
func (g gitSigning) signer(env *sys.OS) github.Signer {
	return func(ctx context.Context, payload []byte) (string, error) {
		ctx, cancel := context.WithTimeout(ctx, signTimeout)
		defer cancel()
		var args []string
		armour := "-----BEGIN PGP SIGNATURE-----"
		switch g.Format {
		case "ssh":
			key, done, err := sshKeyFile(env, g.Key)
			if err != nil {
				return "", err
			}
			defer done()
			args, armour = []string{"-Y", "sign", "-n", "git", "-f", key}, "-----BEGIN SSH SIGNATURE-----"
		default:
			args = []string{"--status-fd=2", "-bsa"}
			if g.Key != "" {
				args = []string{"--status-fd=2", "-bsau", g.Key}
			}
		}
		cmd := exec.CommandContext(ctx, g.Program, args...)
		cmd.Stdin = bytes.NewReader(payload)
		var out, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &stderr
		cmd.WaitDelay = 5 * time.Second
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("%s: %v%s", filepath.Base(g.Program), err, lastLine(stderr.String()))
		}
		sig := out.String()
		if !strings.HasPrefix(strings.TrimSpace(sig), armour) {
			return "", fmt.Errorf("%s returned no signature", filepath.Base(g.Program))
		}
		return sig, nil
	}
}

// sshKeyFile is the key for ssh-keygen -f: the configured path (a .pub makes
// ssh-keygen use the agent), or a literal key written to a private temporary
// file for the one call.
func sshKeyFile(env *sys.OS, key string) (string, func(), error) {
	if lit, ok := literalSSHKey(key); ok {
		dir, done, err := sys.PrivateTempDir("push")
		if err != nil {
			return "", func() {}, err
		}
		p := filepath.Join(dir, "signing-key.pub")
		if err := os.WriteFile(p, []byte(lit+"\n"), 0o600); err != nil {
			done()
			return "", func() {}, err
		}
		return p, done, nil
	}
	if rest, ok := strings.CutPrefix(key, "~/"); ok {
		key = filepath.Join(env.Home(), rest)
	}
	if _, err := os.Stat(key); err != nil {
		return "", func() {}, fmt.Errorf("the signing key in your git config (user.signingkey) is missing: %s", key)
	}
	return key, func() {}, nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if l := strings.TrimSpace(lines[len(lines)-1]); l != "" {
		return ": " + l
	}
	return ""
}

// signingNote explains a push's signing outcome in one line, or "" when the
// commit is Verified or signing is not set up.
func signingNote(s github.SignResult, g gitSigning) string {
	switch {
	case s.Err != nil:
		return fmt.Sprintf("Could not sign the commit (%v); it went up unsigned.", s.Err)
	case !s.Signed || s.Verified:
		return ""
	}
	switch s.Reason {
	case "unknown_key":
		if g.Format == "ssh" {
			return "Signed, but GitHub does not know this key as a signing key. Add " + g.describe() + " at https://github.com/settings/ssh/new with Key type: Signing Key."
		}
		return "Signed, but GitHub does not know this GPG key. Add it at https://github.com/settings/gpg/new."
	case "no_user", "unverified_email", "bad_email":
		return "Signed, but GitHub could not match the key to your account (" + s.Reason + "). The key must belong to the account you signed in as."
	}
	return "Signed, but GitHub did not verify the signature (" + cmp.Or(s.Reason, "no reason given") + ")."
}
