package backup

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/doguyilmaz/dothaven/internal/registry"
)

func fill(files map[string]string, exec map[string]bool) func(Sink) error {
	return func(s Sink) error {
		for dest, body := range files {
			if err := s.Add(dest, []byte(body), exec[dest]); err != nil {
				return err
			}
		}
		return nil
	}
}

func pass(p string) func() (string, error) { return func() (string, error) { return p, nil } }

// An encrypted backup has to restore. This is the round trip the old code
// failed (the decrypted tarball sat beside the extracted tree, so the root was
// misread and restore found nothing).
func TestWriteArchiveEncryptedRoundTrip(t *testing.T) {
	d := t.TempDir()
	dst := filepath.Join(d, "backup-box-1.tar.gz.age")
	files := map[string]string{"ssh/id_ed25519": "-----BEGIN OPENSSH PRIVATE KEY-----\nk\n", "git/hooks/pre-commit": "#!/bin/sh\n"}
	if err := WriteEncryptedArchive(dst, "backup-box-1", "correct horse battery", fill(files, map[string]bool{"git/hooks/pre-commit": true})); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("archive mode %o, want 600", fi.Mode().Perm())
	}
	if _, err := os.Stat(dst + ".partial"); !os.IsNotExist(err) {
		t.Error("partial file left behind")
	}
	raw, _ := os.ReadFile(dst)
	if bytes.Contains(raw, []byte("PRIVATE KEY")) {
		t.Fatal("plaintext visible in encrypted archive")
	}

	root, err := ExtractArchive(dst, t.TempDir(), pass("correct horse battery"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(root) != "backup-box-1" {
		t.Fatalf("root = %s, want backup-box-1", root)
	}
	got, _ := os.ReadFile(filepath.Join(root, "ssh", "id_ed25519"))
	if !strings.Contains(string(got), "PRIVATE KEY") {
		t.Errorf("key did not survive: %q", got)
	}
	hook, err := os.Stat(filepath.Join(root, "git", "hooks", "pre-commit"))
	if err != nil || hook.Mode().Perm()&0o100 == 0 {
		t.Errorf("hook lost its executable bit: %v", hook.Mode())
	}
}

func TestExtractArchiveWrongPassphrase(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "b.tar.gz.age")
	if err := WriteEncryptedArchive(dst, "b", "right passphrase", fill(map[string]string{"x": "1"}, nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := ExtractArchive(dst, t.TempDir(), pass("wrong passphrase")); !errors.Is(err, ErrWrongPassphrase) {
		t.Errorf("err = %v, want ErrWrongPassphrase", err)
	}
}

func TestWriteArchivePlainAndAbort(t *testing.T) {
	d := t.TempDir()
	dst := filepath.Join(d, "b.tar.gz")
	if err := WriteArchive(dst, "b", fill(map[string]string{"shell/.zshrc": "alias ll=ls"}, nil)); err != nil {
		t.Fatal(err)
	}
	root, err := ExtractArchive(dst, t.TempDir(), func() (string, error) {
		t.Fatal("a plain archive must not ask for a passphrase")
		return "", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "shell", ".zshrc")); string(b) != "alias ll=ls" {
		t.Errorf("content = %q", b)
	}

	// A fill that fails leaves nothing behind that looks like a backup.
	bad := filepath.Join(d, "bad.tar.gz")
	if err := WriteArchive(bad, "b", func(Sink) error { return ErrNothingToWrite }); !errors.Is(err, ErrNothingToWrite) {
		t.Fatalf("err = %v", err)
	}
	for _, p := range []string{bad, bad + ".partial"} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s left behind", filepath.Base(p))
		}
	}
}

// The archive is age's own format, so the age CLI can open it. Getting your own
// files back does not depend on dothaven.
func TestEncryptedArchiveOpensWithAgeCLI(t *testing.T) {
	if _, err := exec.LookPath("age"); err != nil {
		t.Skip("age not installed")
	}
	dst := filepath.Join(t.TempDir(), "b.tar.gz.age")
	if err := WriteEncryptedArchive(dst, "b", "correct horse battery", fill(map[string]string{"x": "1"}, nil)); err != nil {
		t.Fatal(err)
	}
	// age reads the passphrase from the terminal only; without one it cannot be
	// driven, so this checks the header age parses before it asks.
	out, _ := exec.Command("age", "--decrypt", dst).CombinedOutput()
	if strings.Contains(string(out), "failed to read header") || strings.Contains(string(out), "unknown format") {
		t.Errorf("age rejected the file: %s", out)
	}
}

func TestWalkFollowsSymlinksAndKeepsExec(t *testing.T) {
	home := t.TempDir()
	dotfiles := filepath.Join(home, "dotfiles", "nvim")
	mustWrite(t, filepath.Join(dotfiles, "init.lua"), "vim.o.number = true")
	mustWrite(t, filepath.Join(home, "skills-repo", "review", "SKILL.md"), "# review")
	// stow-style: the config dir itself is a link.
	os.MkdirAll(filepath.Join(home, ".config"), 0o755)
	os.Symlink(dotfiles, filepath.Join(home, ".config", "nvim"))
	// a skill linked in from a repo, plus a cycle back to the root.
	mustWrite(t, filepath.Join(home, ".claude", "skills", "local", "SKILL.md"), "# local")
	os.Symlink(filepath.Join(home, "skills-repo", "review"), filepath.Join(home, ".claude", "skills", "review"))
	os.Symlink(filepath.Join(home, ".claude", "skills"), filepath.Join(home, ".claude", "skills", "local", "loop"))
	hook := filepath.Join(home, ".claude", "skills", "local", "run.sh")
	mustWrite(t, hook, "#!/bin/sh\n")
	os.Chmod(hook, 0o755)

	files, _ := Walk(registry.BackupTarget{Src: filepath.Join(home, ".config", "nvim"), Dest: "editor/nvim", IsDir: true}, WalkOptions{})
	if len(files) != 1 || files[0].Dest != "editor/nvim/init.lua" {
		t.Errorf("symlinked root: %+v", files)
	}

	files, _ = Walk(registry.BackupTarget{Src: filepath.Join(home, ".claude", "skills"), Dest: "ai/claude/skills", IsDir: true}, WalkOptions{})
	got := map[string]bool{}
	for _, f := range files {
		got[f.Dest] = f.Exec
	}
	for _, want := range []string{"ai/claude/skills/local/SKILL.md", "ai/claude/skills/review/SKILL.md", "ai/claude/skills/local/run.sh"} {
		if _, ok := got[want]; !ok {
			t.Errorf("missing %s in %v", want, got)
		}
	}
	if !got["ai/claude/skills/local/run.sh"] {
		t.Error("run.sh lost its executable bit")
	}
	if len(files) != 3 {
		t.Errorf("cycle walked more than once: %d files", len(files))
	}
}

func TestWalkReportsOversizedAndHonorsExcludes(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, ".fly", "config.yml"), "access_token: x")
	mustWrite(t, filepath.Join(home, ".fly", "bin", "flyctl"), strings.Repeat("x", 100))
	mustWrite(t, filepath.Join(home, ".fly", "big.db"), strings.Repeat("x", 2048))
	mustWrite(t, filepath.Join(home, ".fly", ".git", "HEAD"), "ref")

	tg := registry.BackupTarget{Src: filepath.Join(home, ".fly"), Dest: "cloud/fly", IsDir: true, Exclude: []string{"bin"}}
	files, skipped := Walk(tg, WalkOptions{MaxSize: 1024})
	var dests []string
	for _, f := range files {
		dests = append(dests, f.Dest)
	}
	if strings.Join(dests, ",") != "cloud/fly/.git/HEAD,cloud/fly/config.yml" {
		t.Errorf("files = %v", dests)
	}
	if len(skipped) != 1 || skipped[0].Dest != "cloud/fly/big.db" || skipped[0].Reason != "too large" {
		t.Errorf("skipped = %+v", skipped)
	}
	files, _ = Walk(tg, WalkOptions{MaxSize: 1024, SkipVCS: true})
	for _, f := range files {
		if strings.Contains(f.Dest, ".git/") {
			t.Errorf("SkipVCS kept %s", f.Dest)
		}
	}
}

func TestExcluded(t *testing.T) {
	cases := []struct {
		rel  string
		pats []string
		want bool
	}{
		{"logs/a.txt", []string{"logs"}, true},
		{"x/logs/a.txt", []string{"logs"}, true},
		{"x/a.log", []string{"*.log"}, true},
		{"bin/flyctl", []string{"bin/flyctl"}, true},
		{"cache/x/y", []string{"cache/"}, true},
		{"x/cache/y", []string{"cache/"}, false},
		{"config.yml", []string{"logs", "*.log"}, false},
	}
	for _, c := range cases {
		if got := Excluded(c.rel, c.pats); got != c.want {
			t.Errorf("Excluded(%q, %v) = %v", c.rel, c.pats, got)
		}
	}
}

// Two entries naming one file (~/.ssh/config alone and inside ~/.ssh) carry it
// once; a key the plaintext gate withholds is reported, not just dropped.
func TestRunDedupesAndReportsWithheld(t *testing.T) {
	home, dest := t.TempDir(), t.TempDir()
	mustWrite(t, filepath.Join(home, ".ssh", "config"), "Host x\n")
	mustWrite(t, filepath.Join(home, ".ssh", "id_ed25519"), "-----BEGIN OPENSSH PRIVATE KEY-----\n")
	targets := []registry.BackupTarget{
		{Src: filepath.Join(home, ".ssh", "config"), Dest: "ssh/config", Category: "ssh"},
		{Src: filepath.Join(home, ".ssh"), Dest: "ssh", Category: "ssh", IsDir: true},
	}
	res, err := Run(targets, dest, Options{Redact: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.TotalFiles != 1 {
		t.Errorf("TotalFiles = %d, want 1 (config once)", res.TotalFiles)
	}
	if len(res.Withheld) != 1 || res.Withheld[0] != "ssh/id_ed25519" {
		t.Errorf("Withheld = %v", res.Withheld)
	}
}

func TestVerifyCountsAndDetectsCorruption(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "b.tar.gz.age")
	if err := WriteEncryptedArchive(dst, "b", "correct horse battery", fill(map[string]string{"a": "1", "b": "2"}, nil)); err != nil {
		t.Fatal(err)
	}
	n, err := Verify(dst, pass("correct horse battery"))
	if err != nil || n != 2 {
		t.Fatalf("Verify = %d, %v", n, err)
	}
	raw, _ := os.ReadFile(dst)
	raw[len(raw)-10] ^= 0xff
	os.WriteFile(dst, raw, 0o600)
	if _, err := Verify(dst, pass("correct horse battery")); err == nil {
		t.Error("a corrupted archive verified")
	}
}

// An include that wraps a credential root does not smuggle the credential into
// a plaintext backup; a registry entry that redacts a file inside the root
// still carries it.
func TestGuardedRootsAgainstIncludes(t *testing.T) {
	home, dest := t.TempDir(), t.TempDir()
	mustWrite(t, filepath.Join(home, ".aws", "config"), "[default]\nregion = eu-west-1\n")
	mustWrite(t, filepath.Join(home, ".aws", "credentials"), "[default]\nopaque = zzzzzzzz\n")
	targets := []registry.BackupTarget{
		{Src: filepath.Join(home, ".aws", "credentials"), Dest: "cloud/aws/credentials", Category: "cloud", Sensitivity: registry.High},
		{Src: filepath.Join(home, ".aws"), Dest: "extra/.aws", Category: registry.ExtraCategory, IsDir: true, Sensitivity: registry.Medium},
	}
	res, err := Run(targets, dest, Options{Redact: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "extra", ".aws", "credentials")); !os.IsNotExist(err) {
		t.Fatal("credentials reached a plaintext backup through an include")
	}
	if _, err := os.Stat(filepath.Join(dest, "extra", ".aws", "config")); err != nil {
		t.Error("the harmless sibling should still be carried")
	}
	// Reported once, under the entry that names it, and not again for the
	// include.
	if len(res.SkippedSensitive) != 1 || len(res.Withheld) != 0 {
		t.Errorf("SkippedSensitive = %v, Withheld = %v", res.SkippedSensitive, res.Withheld)
	}
}

// A credential file that is a symlink (stow, chezmoi symlink mode) is still a
// credential when the folder it really lives in is what gets included. So is
// a symlink in an included folder that points at one.
func TestGuardedRootsThroughSymlinks(t *testing.T) {
	home, dest := t.TempDir(), t.TempDir()
	mustWrite(t, filepath.Join(home, ".dotfiles", "kube", "config"), "users:\n- user:\n    token: opaque-zzzz\n")
	mustWrite(t, filepath.Join(home, ".dotfiles", "zshrc"), "alias ll=ls\n")
	os.MkdirAll(filepath.Join(home, ".kube"), 0o700)
	if err := os.Symlink(filepath.Join(home, ".dotfiles", "kube", "config"), filepath.Join(home, ".kube", "config")); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(home, ".aws", "credentials"), "[default]\nopaque = zzzzzzzz\n")
	os.MkdirAll(filepath.Join(home, "notes"), 0o700)
	if err := os.Symlink(filepath.Join(home, ".aws", "credentials"), filepath.Join(home, "notes", "aws")); err != nil {
		t.Fatal(err)
	}
	targets := []registry.BackupTarget{
		{Src: filepath.Join(home, ".kube", "config"), Dest: "cloud/kube/config", Category: "cloud", Sensitivity: registry.High},
		{Src: filepath.Join(home, ".aws", "credentials"), Dest: "cloud/aws/credentials", Category: "cloud", Sensitivity: registry.High},
		{Src: filepath.Join(home, ".dotfiles"), Dest: "extra/.dotfiles", Category: registry.ExtraCategory, IsDir: true, Sensitivity: registry.Medium},
		{Src: filepath.Join(home, "notes"), Dest: "extra/notes", Category: registry.ExtraCategory, IsDir: true, Sensitivity: registry.Medium},
	}
	if _, err := Run(targets, dest, Options{Redact: true}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"extra/.dotfiles/kube/config", "extra/notes/aws"} {
		if _, err := os.Stat(filepath.Join(dest, p)); !os.IsNotExist(err) {
			t.Errorf("%s reached a plaintext backup through a symlink", p)
		}
	}
	if _, err := os.Stat(filepath.Join(dest, "extra", ".dotfiles", "zshrc")); err != nil {
		t.Error("the harmless file beside it should still be carried")
	}
}

// A push to GitHub leaves age identities behind (the registry's, one inside a
// folder that wraps it, and one under any other name) even when encrypted. A
// local encrypted backup carries them.
func TestRemoteRunKeepsAgeKeysLocal(t *testing.T) {
	home := t.TempDir()
	key := "AGE-SECRET-KEY-1QYQSZQGPQYQSZQGPQYQSZQGPQYQSZQGPQYQSZQGPQYQSZQGPQYQSZQGPQY\n"
	mustWrite(t, filepath.Join(home, ".config", "chezmoi", "key.txt"), key)
	mustWrite(t, filepath.Join(home, ".config", "chezmoi", "chezmoi.toml"), "encryption = \"age\"\n")
	mustWrite(t, filepath.Join(home, "keys", "other-name"), key)
	mustWrite(t, filepath.Join(home, ".zshrc"), "alias ll=ls\n")
	targets := []registry.BackupTarget{
		{Src: filepath.Join(home, ".config", "chezmoi", "key.txt"), Dest: "secrets/age/chezmoi-key.txt", Category: "secrets", Sensitivity: registry.High, LocalOnly: true},
		{Src: filepath.Join(home, ".config", "chezmoi"), Dest: "dev/chezmoi", Category: "dev", IsDir: true, Sensitivity: registry.Medium},
		{Src: filepath.Join(home, "keys"), Dest: "extra/keys", Category: registry.ExtraCategory, IsDir: true, Sensitivity: registry.Medium},
		{Src: filepath.Join(home, ".zshrc"), Dest: "shell/.zshrc", Category: "shell", Sensitivity: registry.Low},
	}
	remote := memSink{}
	res, err := RunTo(targets, remote, Options{Encrypted: true, Remote: true})
	if err != nil {
		t.Fatal(err)
	}
	for dest, body := range remote {
		if strings.Contains(body, "AGE-SECRET-KEY-1") {
			t.Errorf("%s carries an age key to GitHub", dest)
		}
	}
	if _, ok := remote["dev/chezmoi/chezmoi.toml"]; !ok {
		t.Error("chezmoi's config should still go")
	}
	if strings.Join(res.KeptLocal, ",") != "secrets/age/chezmoi-key.txt,extra/keys/other-name" {
		t.Errorf("KeptLocal = %v", res.KeptLocal)
	}
	local := memSink{}
	if _, err := RunTo(targets, local, Options{Encrypted: true}); err != nil {
		t.Fatal(err)
	}
	if local["secrets/age/chezmoi-key.txt"] != key {
		t.Error("a local encrypted backup must carry the age key")
	}
}

type memSink map[string]string

func (m memSink) Add(dest string, data []byte, _ bool) error { m[dest] = string(data); return nil }

// Split mode: config that is safe to read goes plain; credential entries,
// files holding a secret, and anything under a credential root go encrypted.
func TestSplitSinkClassifies(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, ".zshrc"), "alias ll='ls -la'\n")
	mustWrite(t, filepath.Join(home, ".bashrc"), "export GITHUB_TOKEN=ghp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n")
	mustWrite(t, filepath.Join(home, ".aws", "credentials"), "[default]\nx = y\n")
	mustWrite(t, filepath.Join(home, ".aws", "config"), "[default]\nregion = eu\n")
	targets := []registry.BackupTarget{
		{Src: filepath.Join(home, ".zshrc"), Dest: "shell/.zshrc", Category: "shell", Sensitivity: registry.Low},
		{Src: filepath.Join(home, ".bashrc"), Dest: "shell/.bashrc", Category: "shell", Sensitivity: registry.Low},
		{Src: filepath.Join(home, ".aws", "credentials"), Dest: "cloud/aws/credentials", Category: "cloud", Sensitivity: registry.High},
		{Src: filepath.Join(home, ".aws", "config"), Dest: "cloud/aws/config", Category: "cloud", Sensitivity: registry.Medium},
	}
	plain, secret := memSink{}, memSink{}
	split := &SplitSink{Plain: plain, Secret: secret}
	if _, err := RunTo(targets, split, Options{Redact: false}); err != nil {
		t.Fatal(err)
	}
	if _, ok := plain["shell/.zshrc"]; !ok {
		t.Error(".zshrc should be plain")
	}
	for _, d := range []string{"shell/.bashrc", "cloud/aws/credentials", "cloud/aws/config"} {
		if _, ok := secret[d]; !ok {
			t.Errorf("%s should be in the encrypted part", d)
		}
		if _, ok := plain[d]; ok {
			t.Errorf("%s leaked into the plain part", d)
		}
	}
	if !strings.Contains(secret["shell/.bashrc"], "ghp_") {
		t.Error("the encrypted part keeps the real value")
	}
}

func TestEncryptedArchiveRefusesEmptyPassphrase(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "b.tar.gz.age")
	err := WriteEncryptedArchive(dst, "b", "", fill(map[string]string{"ssh/id_ed25519": "KEY"}, nil))
	if !errors.Is(err, ErrNoPassphrase) {
		t.Fatalf("err = %v, want ErrNoPassphrase", err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Error("an archive was written anyway")
	}
	if _, err := os.Stat(dst + ".partial"); !os.IsNotExist(err) {
		t.Error("a partial file was left behind")
	}
}

func TestAgeHeaderChecksPassphrase(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "b.tar.gz.age")
	if err := WriteEncryptedArchive(dst, "b", "correct horse battery", fill(map[string]string{"x": strings.Repeat("y", 1<<20)}, nil)); err != nil {
		t.Fatal(err)
	}
	hdr, err := AgeHeader(dst)
	if err != nil {
		t.Fatal(err)
	}
	if len(hdr) > 1024 {
		t.Errorf("header is %d bytes; it should be the header, not the file", len(hdr))
	}
	if !HeaderOpens(hdr, "correct horse battery") {
		t.Error("the right passphrase does not open the header")
	}
	if HeaderOpens(hdr, "a different passphrase") {
		t.Error("a wrong passphrase opens the header")
	}
	plain := filepath.Join(t.TempDir(), "b.tar.gz")
	if err := WriteArchive(plain, "b", fill(map[string]string{"x": "1"}, nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := AgeHeader(plain); err == nil {
		t.Error("a plain archive has an age header")
	}
}

// A command that needs only the inventory puts only the inventory on disk:
// the decrypted SSH key in the same archive is read past, never written.
func TestExtractOnlyWritesWhatWasAskedFor(t *testing.T) {
	src := filepath.Join(t.TempDir(), "b.tar.gz.age")
	files := map[string]string{"ssh/id_ed25519": "-----BEGIN OPENSSH PRIVATE KEY-----\n", "inventory/snapshot.json": "{}\n", "MANIFEST.txt": "#\n"}
	if err := WriteEncryptedArchive(src, "backup-box-1", "correct horse battery", fill(files, nil)); err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	root, err := ExtractArchiveOnly(src, dst, pass("correct horse battery"), Only("inventory"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "inventory", "snapshot.json")); err != nil {
		t.Errorf("inventory not extracted: %v", err)
	}
	var written []string
	filepath.WalkDir(dst, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			written = append(written, p)
		}
		return nil
	})
	if len(written) != 1 {
		t.Errorf("wrote %v, want only the inventory", written)
	}
}

// A registry folder (not an include) holding a link into a credential folder
// does not carry the credential into a plaintext backup either.
func TestRegistryDirLinkIntoCredentialRoot(t *testing.T) {
	home, dest := t.TempDir(), t.TempDir()
	mustWrite(t, filepath.Join(home, ".aws", "credentials"), "[default]\nopaque = zzzzzzzz\n")
	mustWrite(t, filepath.Join(home, ".config", "tool", "settings.json"), "{}\n")
	if err := os.Symlink(filepath.Join(home, ".aws", "credentials"), filepath.Join(home, ".config", "tool", "creds")); err != nil {
		t.Fatal(err)
	}
	targets := []registry.BackupTarget{
		{Src: filepath.Join(home, ".aws", "credentials"), Dest: "cloud/aws/credentials", Category: "cloud", Sensitivity: registry.High},
		{Src: filepath.Join(home, ".config", "tool"), Dest: "apps/tool", Category: "apps", IsDir: true, Sensitivity: registry.Low},
	}
	if _, err := Run(targets, dest, Options{Redact: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "apps", "tool", "creds")); !os.IsNotExist(err) {
		t.Error("credentials reached a plaintext backup through a link in a registry folder")
	}
	if _, err := os.Stat(filepath.Join(dest, "apps", "tool", "settings.json")); err != nil {
		t.Error("the folder's own config should still be carried")
	}
}

// What a remote run writes itself (an inventory, a crontab) has any age
// identity masked, and a split sink still sees how the file was classified.
func TestAgeMaskSink(t *testing.T) {
	key := "AGE-SECRET-KEY-1QYQSZQGPQYQSZQGPQYQSZQGPQYQSZQGPQYQSZQGPQYQSZQGPQYQSZQGPQY"
	plain, secret := memSink{}, memSink{}
	m := AgeMaskSink{Inner: &SplitSink{Plain: plain, Secret: secret}}
	if err := m.Add("inventory/crontab", []byte("0 * * * * SOPS_AGE_KEY="+key+" sync\n"), false); err != nil {
		t.Fatal(err)
	}
	if err := m.AddClassified("macos-defaults/prefs.json", []byte(`{"v":"`+key+`"}`), false, true); err != nil {
		t.Fatal(err)
	}
	for _, s := range []memSink{plain, secret} {
		for d, b := range s {
			if strings.Contains(b, "AGE-SECRET-KEY-1") {
				t.Errorf("%s still holds the identity", d)
			}
		}
	}
	if _, ok := secret["macos-defaults/prefs.json"]; !ok {
		t.Error("classification lost through the mask")
	}
}
