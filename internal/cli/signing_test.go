package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/doguyilmaz/dothaven/internal/github"
	"github.com/doguyilmaz/dothaven/internal/sys"
)

// gitHome gives git a fresh global config and no system one.
func gitHome(t *testing.T, config string) (*sys.OS, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	return sys.Real(), home
}

func need(t *testing.T, tools ...string) map[string]string {
	t.Helper()
	paths := map[string]string{}
	for _, tool := range tools {
		p, err := exec.LookPath(tool)
		if err != nil {
			t.Skipf("%s not installed", tool)
		}
		paths[tool] = p
	}
	return paths
}

func run(t *testing.T, dir, stdin string, args ...string) string {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir, cmd.Stdin = dir, strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// verifyWithGit stores payload+signature as a real commit object and has git
// check it. The layout is what GitHub builds from the API fields: gpgsig right
// after the committer line, continuation lines indented.
func verifyWithGit(t *testing.T, repo string, payload []byte, sig string, verify ...string) {
	t.Helper()
	head, body, _ := strings.Cut(string(payload), "\n\n")
	obj := head + "\ngpgsig " + strings.ReplaceAll(strings.TrimRight(sig, "\n"), "\n", "\n ") + "\n\n" + body
	sha := run(t, repo, obj, "git", "hash-object", "-t", "commit", "-w", "--stdin")
	run(t, repo, "", append(append([]string{"git"}, verify...), "verify-commit", sha)...)
	tampered := strings.Replace(obj, "1 file", "2 files", 1)
	bad := run(t, repo, tampered, "git", "hash-object", "-t", "commit", "-w", "--stdin")
	cmd := exec.Command("git", append(verify, "verify-commit", bad)...)
	cmd.Dir = repo
	if cmd.Run() == nil {
		t.Fatal("a tampered commit verified")
	}
}

func signPayload(t *testing.T, env *sys.OS, repo string) ([]byte, string) {
	t.Helper()
	g, ok := loadGitSigning(context.Background())
	if !ok {
		t.Fatal("git signs commits here, but loadGitSigning says no")
	}
	tree := run(t, repo, "", "git", "hash-object", "-t", "tree", "-w", "--stdin")
	bot := github.Identity{Name: "dothaven[bot]", Email: "2002+dothaven[bot]@users.noreply.github.com"}
	you := github.NoReply("tester", 1001)
	payload := github.CommitPayload(tree, "", bot, you, time.Now(), "dothaven: box (encrypted backup, 1 file)\n")
	sig, err := g.signer(env)(context.Background(), payload)
	if err != nil {
		t.Fatal(err)
	}
	return payload, sig
}

func TestSignsLikeGitWithSSH(t *testing.T) {
	tools := need(t, "git", "ssh-keygen")
	dir := t.TempDir()
	key := filepath.Join(dir, "id_ed25519")
	run(t, dir, "", "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "test", "-f", key)
	env, home := gitHome(t, "[commit]\n\tgpgsign = true\n[gpg]\n\tformat = ssh\n[gpg \"ssh\"]\n\tprogram = "+tools["ssh-keygen"]+"\n[user]\n\tsigningkey = "+key+".pub\n")
	repo := filepath.Join(home, "repo")
	run(t, home, "", "git", "init", "-q", repo)

	payload, sig := signPayload(t, env, repo)
	pub, _ := os.ReadFile(key + ".pub")
	allowed := filepath.Join(dir, "allowed")
	os.WriteFile(allowed, []byte("1001+tester@users.noreply.github.com "+string(pub)), 0o600)
	verifyWithGit(t, repo, payload, sig, "-c", "gpg.ssh.allowedSignersFile="+allowed)

	// A key written into the config itself, as `key::ssh-ed25519 …`.
	os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("[commit]\n\tgpgsign = true\n[gpg]\n\tformat = ssh\n[gpg \"ssh\"]\n\tprogram = "+tools["ssh-keygen"]+"\n[user]\n\tsigningkey = key::"+strings.TrimSpace(string(pub))+"\n"), 0o600)
	g, ok := loadGitSigning(context.Background())
	if !ok || !strings.HasPrefix(g.describe(), "SSH key (ssh-ed25519") {
		t.Errorf("literal key: %+v %v, describe = %q", g, ok, g.describe())
	}
	f, done, err := sshKeyFile(env, g.Key)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(f); strings.TrimSpace(string(b)) != strings.TrimSpace(string(pub)) {
		t.Errorf("literal key file holds %q", b)
	}
	done()
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Error("the literal key's temporary file outlived the signing")
	}
}

func TestSignsLikeGitWithGPG(t *testing.T) {
	tools := need(t, "git", "gpg")
	gnupg := t.TempDir()
	t.Setenv("GNUPGHOME", gnupg)
	run(t, gnupg, "", "gpg", "--batch", "--pinentry-mode", "loopback", "--passphrase", "", "--quick-gen-key", "Tester <1001+tester@users.noreply.github.com>", "ed25519", "sign", "never")
	env, home := gitHome(t, "[commit]\n\tgpgsign = true\n[gpg]\n\tprogram = "+tools["gpg"]+"\n[user]\n\tsigningkey = 1001+tester@users.noreply.github.com\n")
	repo := filepath.Join(home, "repo")
	run(t, home, "", "git", "init", "-q", repo)

	payload, sig := signPayload(t, env, repo)
	if !strings.HasPrefix(sig, "-----BEGIN PGP SIGNATURE-----") {
		t.Fatalf("sig = %q", sig)
	}
	verifyWithGit(t, repo, payload, sig)
}

func TestGitSigningOnlyWhenGitSigns(t *testing.T) {
	need(t, "git")
	for name, config := range map[string]string{
		"not signing":         "[user]\n\tsigningkey = ~/.ssh/id.pub\n[gpg]\n\tformat = ssh\n",
		"ssh without a key":   "[commit]\n\tgpgsign = true\n[gpg]\n\tformat = ssh\n",
		"x509 (not drivable)": "[commit]\n\tgpgsign = true\n[gpg]\n\tformat = x509\n",
	} {
		gitHome(t, config)
		if _, ok := loadGitSigning(context.Background()); ok {
			t.Errorf("%s: dothaven would sign", name)
		}
	}
	env, _ := gitHome(t, "[commit]\n\tgpgsign = true\n[gpg]\n\tformat = ssh\n[user]\n\tsigningkey = ~/.ssh/missing.pub\n")
	g, ok := loadGitSigning(context.Background())
	if !ok || g.Program != "ssh-keygen" {
		t.Fatalf("g = %+v %v", g, ok)
	}
	if _, err := g.signer(env)(context.Background(), []byte("x")); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Errorf("missing key: %v", err)
	}
}

func TestSigningNotes(t *testing.T) {
	ssh := gitSigning{Format: "ssh", Key: "~/.ssh/id_ed25519.pub"}
	for _, c := range []struct {
		res  github.SignResult
		want string
	}{
		{github.SignResult{}, ""},
		{github.SignResult{Signed: true, Verified: true, Reason: "valid"}, ""},
		{github.SignResult{Signed: true, Reason: "unknown_key"}, "Key type: Signing Key"},
		{github.SignResult{Signed: true, Reason: "unverified_email"}, "account you signed in as"},
		{github.SignResult{Signed: true, Reason: "invalid"}, "(invalid)"},
	} {
		got := signingNote(c.res, ssh)
		if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
			t.Errorf("%+v: %q, want %q", c.res, got, c.want)
		}
	}
}
