package chezmoi

import (
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/doguyilmaz/dothaven/internal/registry"
	"github.com/doguyilmaz/dothaven/internal/scan"
	"github.com/doguyilmaz/dothaven/internal/snapshot"
)

func TestIsSelected(t *testing.T) {
	if !IsSelected("shell", nil, nil) {
		t.Error("empty filters → selected")
	}
	if IsSelected("shell", nil, []string{"shell"}) {
		t.Error("skip should win")
	}
	if IsSelected("npm", []string{"shell"}, nil) {
		t.Error("only-list should exclude non-members")
	}
	if !IsSelected("shell", []string{"shell"}, nil) {
		t.Error("only-list member should be selected")
	}
}

func TestPlanExportEncryptDecision(t *testing.T) {
	entries := []registry.Entry{
		{ID: "shell.zshrc", Category: "shell", Kind: registry.File, Sensitivity: registry.Low, Paths: platPath("/h/.zshrc")},
		{ID: "cloud.kube", Category: "cloud", Kind: registry.File, Sensitivity: registry.High, Paths: platPath("/h/.kube/config")},
		{ID: "ssh.config", Category: "ssh", Kind: registry.File, Sensitivity: registry.Medium, Redact: scan.RedactSSHConfig, Paths: platPath("/h/.ssh/config")},
		{ID: "git.config", Category: "git", Kind: registry.File, Sensitivity: registry.Low, Paths: platPath("/h/.gitconfig")},
		{ID: "absent", Category: "x", Kind: registry.File, Paths: platPath("/h/none")},
	}
	exists := func(p string) bool { return p != "/h/none" }
	// gitconfig hides a real secret; everything else is clean.
	secret := func(p string, isDir bool) bool { return p == "/h/.gitconfig" }

	plan := PlanExport(entries, "/h", exists, secret)
	got := map[string]PlanItem{}
	for _, p := range plan {
		got[p.ID] = p
	}

	if len(plan) != 4 {
		t.Fatalf("plan length = %d, want 4 (absent dropped)", len(plan))
	}
	if got["shell.zshrc"].Encrypt {
		t.Error("low-sensitivity clean config should be plain")
	}
	if !got["cloud.kube"].Encrypt || got["cloud.kube"].Reason != "sensitivity:high" {
		t.Errorf("high sensitivity → encrypt: %+v", got["cloud.kube"])
	}
	if !got["ssh.config"].Encrypt || got["ssh.config"].Reason != "has redact rule" {
		t.Errorf("redact rule → encrypt: %+v", got["ssh.config"])
	}
	if !got["git.config"].Encrypt || got["git.config"].Reason != "secret detected" {
		t.Errorf("probe-detected secret → encrypt: %+v", got["git.config"])
	}
}

func TestFindSshPrivateKeys(t *testing.T) {
	listDir := func(p string) ([]string, error) {
		return []string{"id_ed25519", "id_ed25519.pub", "config", "id_rsa"}, nil
	}
	isKey := func(p string) bool { return strings.HasSuffix(p, "id_ed25519") || strings.HasSuffix(p, "id_rsa") }
	got := FindSshPrivateKeys("/h", listDir, isKey)
	want := []string{"/h/.ssh/id_ed25519", "/h/.ssh/id_rsa"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FindSshPrivateKeys = %v, want %v", got, want)
	}
}

func TestGnupgHasSecretKeys(t *testing.T) {
	withKey := func(p string) ([]string, error) { return []string{"ABCD.key", "README"}, nil }
	noKey := func(p string) ([]string, error) { return []string{"README"}, nil }
	if !GnupgHasSecretKeys("/h", withKey) {
		t.Error("a *.key should count as secret keys")
	}
	if GnupgHasSecretKeys("/h", noKey) {
		t.Error("no *.key → no secret keys")
	}
}

func TestMergeChezmoiignore(t *testing.T) {
	out := MergeChezmoiignore("", GnupgIgnorePatterns())
	for _, p := range GnupgIgnorePatterns() {
		if !strings.Contains(out, p) {
			t.Errorf("merged ignore missing %q", p)
		}
	}
	// Idempotent: merging again adds nothing.
	if again := MergeChezmoiignore(out, GnupgIgnorePatterns()); again != out {
		t.Errorf("merge not idempotent:\n%q\nvs\n%q", out, again)
	}
	// Preserves existing content.
	existing := "*.tmp\n"
	merged := MergeChezmoiignore(existing, []string{".gnupg/S.*"})
	if !strings.Contains(merged, "*.tmp") || !strings.Contains(merged, ".gnupg/S.*") {
		t.Errorf("merge dropped existing content: %q", merged)
	}
}

func TestFilterBrewfile(t *testing.T) {
	bf := "tap \"x/y\"\nbrew \"ripgrep\"\nvscode \"ms.python\"\ncask \"firefox\"\n"
	got := FilterBrewfile(bf, []string{"vscode"})
	if strings.Contains(got, "vscode") {
		t.Errorf("vscode line not stripped: %q", got)
	}
	if !strings.Contains(got, "ripgrep") || !strings.Contains(got, "firefox") {
		t.Errorf("non-skipped lines dropped: %q", got)
	}
	if FilterBrewfile(bf, nil) != bf {
		t.Error("empty skip should return input unchanged")
	}
}

func TestPickInstallSpec(t *testing.T) {
	it := snapshot.Item{Raw: "typescript@5.4.0", Columns: []string{"typescript", "5.4.0"}}
	if got := PickInstallSpec(it, false); got != "typescript" {
		t.Errorf("unpinned = %q, want bare name", got)
	}
	if got := PickInstallSpec(it, true); got != "typescript@5.4.0" {
		t.Errorf("pinned = %q, want name@version", got)
	}
	bare := snapshot.Item{Raw: "deno-bin"}
	if got := PickInstallSpec(bare, false); got != "deno-bin" {
		t.Errorf("no columns falls back to raw, got %q", got)
	}
}

func TestCrossManagerDuplicates(t *testing.T) {
	m := Manifest{
		BunGlobals:  []string{"argent", "typescript"},
		NpmGlobals:  []string{"argent", "eslint"},
		PnpmGlobals: []string{"prettier"},
	}
	if got := CrossManagerDuplicates(m); !reflect.DeepEqual(got, []string{"argent"}) {
		t.Errorf("duplicates = %v, want [argent]", got)
	}
}

func TestBuildPackageInstallScript(t *testing.T) {
	if _, ok := BuildPackageInstallScript(Manifest{}); ok {
		t.Error("empty manifest should produce no script")
	}
	// deno-only is not installable → no script (deno can't be reconstructed).
	if _, ok := BuildPackageInstallScript(Manifest{DenoBins: []string{"x"}}); ok {
		t.Error("deno-only manifest should produce no script")
	}

	script, ok := BuildPackageInstallScript(Manifest{
		Brewfile:     "brew \"ripgrep\"",
		NodeVersions: []string{"v20.0.0", "system"},
		BunGlobals:   []string{"argent"},
		CargoCrates:  []string{"ripgrep"},
		DenoBins:     []string{"deployctl"},
	})
	if !ok {
		t.Fatal("expected a script")
	}
	for _, want := range []string{
		"#!/bin/bash", "set -uo pipefail",
		"command -v brew", "brew bundle --file=/dev/stdin", "BREWFILE",
		"command -v fnm", "fnm install v20.0.0 || true",
		"command -v bun", "bun add -g argent || true",
		"command -v cargo", "cargo install ripgrep || true",
		"# deno global bins", "#   deployctl",
		"exit 0",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("script missing %q\n---\n%s", want, script)
		}
	}
	if strings.Contains(script, "fnm install system") {
		t.Error("`system` node version must be filtered out")
	}
}

// platPath builds a single-platform Paths map for the running OS so ResolvePath
// resolves in tests regardless of GOOS.
func platPath(p string) map[string]string {
	return map[string]string{runtime.GOOS: p}
}
