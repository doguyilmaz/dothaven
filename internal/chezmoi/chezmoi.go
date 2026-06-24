// Package chezmoi plans a hybrid export: which config sources chezmoi adds plain
// vs. --encrypt, and a run_onchange install script that reinstalls packages on
// `chezmoi apply`. The planning logic is pure (probes are injected); only the
// scan-backed probes touch the filesystem.
package chezmoi

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/doguyilmaz/dothaven/internal/registry"
	"github.com/doguyilmaz/dothaven/internal/scan"
	"github.com/doguyilmaz/dothaven/internal/snapshot"
)

// PlanItem is one path chezmoi will add, with the encrypt/template decision and
// why. Encrypt and Template are mutually exclusive: secrets are encrypted, plain
// host-varying configs are templated, everything else is added verbatim.
type PlanItem struct {
	ID       string
	Src      string
	Kind     string // "file" | "dir"
	Encrypt  bool
	Template bool
	Reason   string
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// IsSelected applies --only/--skip: skip wins; a non-empty only-list restricts.
func IsSelected(category string, only, skip []string) bool {
	if contains(skip, category) {
		return false
	}
	return len(only) == 0 || contains(only, category)
}

// PlanExport decides, per registry entry that exists on disk, whether chezmoi
// adds it plain or --encrypt. Encrypts when the entry is high-sensitivity, has a
// redact rule, or the injected probe finds a real secret (including inside a dir).
func PlanExport(entries []registry.Entry, home string, fileExists func(string) bool, containsSecret func(path string, isDir bool) bool) []PlanItem {
	var items []PlanItem
	for _, e := range entries {
		src := registry.ResolvePath(e, home)
		if src == "" || !fileExists(src) {
			continue
		}
		isDir := e.Kind == registry.Dir
		encrypt := e.Sensitivity == registry.High || e.Redact != nil
		reason := "plain"
		if e.Sensitivity == registry.High {
			reason = "sensitivity:high"
		} else if encrypt {
			reason = "has redact rule"
		}
		if !encrypt && containsSecret(src, isDir) {
			encrypt = true
			reason = "secret detected"
		}
		// A plain, host-varying config is added as a template so its absolute
		// home paths port to a new machine. Never both — secrets are encrypted.
		template := false
		if !encrypt && ShouldTemplate(e) {
			template = true
			reason = "templated (host paths)"
		}
		kind := "file"
		if isDir {
			kind = "dir"
		}
		items = append(items, PlanItem{ID: e.ID, Src: src, Kind: kind, Encrypt: encrypt, Template: template, Reason: reason})
	}
	return items
}

// syncStateEditors are editor settings entries whose app also syncs the same
// files from the cloud (VS Code / Cursor "Settings Sync").
var syncStateEditors = map[string]bool{"editor.vscode.settings": true, "editor.cursor": true}

// SettingsSyncConflicts reports plan sources whose editor has built-in cloud
// Settings Sync active (a sibling `sync/` state dir next to the settings file).
// chezmoi apply and that sync will both rewrite the file, producing endless
// drift, so the user should disable one. exists is injected (env.Exists).
func SettingsSyncConflicts(plan []PlanItem, exists func(string) bool) []string {
	var hits []string
	for _, p := range plan {
		if syncStateEditors[p.ID] && exists(filepath.Join(filepath.Dir(p.Src), "sync")) {
			hits = append(hits, p.Src)
		}
	}
	return hits
}

func anyHigh(findings []scan.Finding) bool {
	for _, f := range findings {
		if f.Pattern.Severity == scan.High {
			return true
		}
	}
	return false
}

// ContainsHighSecret reports a HIGH-severity secret in a file (or any file in a
// directory). HIGH-only so a benign IP/email never forces encryption.
func ContainsHighSecret(path string, isDir bool) bool {
	if isDir {
		for _, r := range scan.ScanDir(path) {
			if anyHigh(r.Findings) {
				return true
			}
		}
		return false
	}
	r := scan.ScanFile(path)
	return r != nil && anyHigh(r.Findings)
}

// IsSSHPrivateKey detects a private key by content (a key header), so it catches
// id_ed25519, id_rsa, custom *.key, etc. — not by filename.
func IsSSHPrivateKey(path string) bool {
	r := scan.ScanFile(path)
	if r == nil {
		return false
	}
	for _, f := range r.Findings {
		if f.Pattern.ID == "private-key-pem" || f.Pattern.ID == "pgp-private-key" {
			return true
		}
	}
	return false
}

// FindSshPrivateKeys sweeps ~/.ssh for private keys by content (skipping .pub).
func FindSshPrivateKeys(home string, listDir func(string) ([]string, error), isPrivateKey func(string) bool) []string {
	dir := home + "/.ssh"
	names, err := listDir(dir)
	if err != nil {
		return nil
	}
	var keys []string
	for _, name := range names {
		if strings.HasSuffix(name, ".pub") {
			continue
		}
		path := dir + "/" + name
		if isPrivateKey(path) {
			keys = append(keys, path)
		}
	}
	sort.Strings(keys)
	return keys
}

// GnupgHasSecretKeys reports whether ~/.gnupg holds real private keys
// (private-keys-v1.d/*.key) — otherwise carrying it captures only runtime cruft.
func GnupgHasSecretKeys(home string, listDir func(string) ([]string, error)) bool {
	names, err := listDir(home + "/.gnupg/private-keys-v1.d")
	if err != nil {
		return false
	}
	for _, f := range names {
		if strings.HasSuffix(f, ".key") {
			return true
		}
	}
	return false
}

// GnupgIgnorePatterns are .chezmoiignore globs for ~/.gnupg runtime cruft —
// sockets, locks, and the RNG seed. Key material is intentionally NOT ignored.
func GnupgIgnorePatterns() []string {
	return []string{".gnupg/S.*", ".gnupg/*.lock", ".gnupg/.#*", ".gnupg/random_seed", ".gnupg/public-keys.d/*.lock"}
}

// MergeChezmoiignore idempotently adds patterns to an existing .chezmoiignore
// under a labeled header. Already-present patterns are left untouched.
func MergeChezmoiignore(existing string, patterns []string) string {
	present := map[string]bool{}
	for _, l := range strings.Split(existing, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			present[t] = true
		}
	}
	header := "# gnupg runtime cruft (managed by dothaven chezmoi-export)"
	var missing []string
	for _, p := range patterns {
		if !present[p] {
			missing = append(missing, p)
		}
	}
	if len(missing) == 0 {
		return existing
	}
	var block []string
	if !present[header] {
		block = append(block, header)
	}
	block = append(block, missing...)
	base := strings.TrimRightFunc(existing, unicode.IsSpace)
	if base != "" {
		return strings.Join(append([]string{base, ""}, block...), "\n") + "\n"
	}
	return strings.Join(block, "\n") + "\n"
}

// FilterBrewfile drops Brewfile lines whose directive (first token) is in skip
// (e.g. --skip vscode strips embedded extension entries).
func FilterBrewfile(brewfile string, skip []string) string {
	if len(skip) == 0 {
		return brewfile
	}
	var kept []string
	for _, line := range strings.Split(brewfile, "\n") {
		directive := ""
		if fields := strings.Fields(line); len(fields) > 0 {
			directive = fields[0]
		}
		if !contains(skip, directive) {
			kept = append(kept, line)
		}
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

// PickInstallSpec is the token to install a package with: the bare name by
// default (fresh machine gets the current release), or the pinned name@version.
func PickInstallSpec(it snapshot.Item, pin bool) string {
	if !pin && len(it.Columns) > 0 {
		return it.Columns[0]
	}
	return it.Raw
}

// Manifest is the set of reinstallable packages captured for the install script.
type Manifest struct {
	Brewfile         string
	NodeVersions     []string
	BunGlobals       []string
	NpmGlobals       []string
	PnpmGlobals      []string
	CargoCrates      []string
	DenoBins         []string
	PipxPackages     []string
	CursorExtensions []string // VS Code extensions ride in the Brewfile; Cursor's don't
	RustToolchains   []string
	UvTools          []string
	ComposerGlobals  []string
	PubGlobals       []string
	DotnetTools      []string
	AptPackages      []string
	DnfPackages      []string
	PacmanPackages   []string
	SnapPackages     []string
	FlatpakPackages  []string
}

// CrossManagerDuplicates are names installed by more than one JS global manager
// (kept in each block, but worth warning about for PATH shadowing).
func CrossManagerDuplicates(m Manifest) []string {
	count := map[string]int{}
	for _, list := range [][]string{m.BunGlobals, m.NpmGlobals, m.PnpmGlobals} {
		seen := map[string]bool{}
		for _, name := range list {
			if !seen[name] {
				seen[name] = true
				count[name]++
			}
		}
	}
	var dupes []string
	for name, n := range count {
		if n > 1 {
			dupes = append(dupes, name)
		}
	}
	sort.Strings(dupes)
	return dupes
}

func guarded(tool string, body ...string) string {
	lines := append([]string{fmt.Sprintf("if command -v %s >/dev/null 2>&1; then", tool)}, body...)
	return strings.Join(append(lines, "fi"), "\n")
}

func installBlock(tool, add string, pkgs []string) (string, bool) {
	if len(pkgs) == 0 {
		return "", false
	}
	body := make([]string, len(pkgs))
	for i, p := range pkgs {
		body[i] = fmt.Sprintf("  %s %s || true", add, p)
	}
	return guarded(tool, body...), true
}

// BuildPackageInstallScript renders a chezmoi run_onchange_ script that reinstalls
// brew formulae/casks, node versions, and global packages on `chezmoi apply`.
// Every step is command-guarded and `|| true`; the script ends in `exit 0` so a
// missing tool never aborts apply. Returns ("", false) when nothing is installable.
func BuildPackageInstallScript(m Manifest) (string, bool) {
	var blocks []string

	if strings.TrimSpace(m.Brewfile) != "" {
		blocks = append(blocks, guarded("brew",
			"  brew bundle --file=/dev/stdin <<'BREWFILE' || true",
			strings.TrimSpace(m.Brewfile),
			"BREWFILE"))
	}

	var versions []string
	for _, v := range m.NodeVersions {
		if v != "" && v != "system" {
			versions = append(versions, v)
		}
	}
	if len(versions) > 0 {
		body := make([]string, len(versions))
		for i, v := range versions {
			body[i] = fmt.Sprintf("  fnm install %s || true", v)
		}
		blocks = append(blocks, guarded("fnm", body...))
	}

	if b, ok := installBlock("bun", "bun add -g", m.BunGlobals); ok {
		blocks = append(blocks, b)
	}
	if b, ok := installBlock("pnpm", "pnpm add -g", m.PnpmGlobals); ok {
		blocks = append(blocks, b)
	}
	if b, ok := installBlock("npm", "npm install -g", m.NpmGlobals); ok {
		blocks = append(blocks, b)
	}

	if len(m.CargoCrates) > 0 {
		body := make([]string, len(m.CargoCrates))
		for i, c := range m.CargoCrates {
			body[i] = fmt.Sprintf("  cargo install %s || true", c)
		}
		blocks = append(blocks, guarded("cargo", body...))
	}

	// Inventory that collect captures but the script used to drop. Each is
	// command-guarded and idempotent, so re-running apply is safe. VS Code
	// extensions are intentionally absent — they ride in the Brewfile already.
	if b, ok := installBlock("pipx", "pipx install", m.PipxPackages); ok {
		blocks = append(blocks, b)
	}
	if b, ok := installBlock("rustup", "rustup toolchain install", m.RustToolchains); ok {
		blocks = append(blocks, b)
	}
	if b, ok := installBlock("cursor", "cursor --install-extension", m.CursorExtensions); ok {
		blocks = append(blocks, b)
	}
	if b, ok := installBlock("uv", "uv tool install", m.UvTools); ok {
		blocks = append(blocks, b)
	}
	if b, ok := installBlock("composer", "composer global require", m.ComposerGlobals); ok {
		blocks = append(blocks, b)
	}
	if b, ok := installBlock("dart", "dart pub global activate", m.PubGlobals); ok {
		blocks = append(blocks, b)
	}
	if b, ok := installBlock("dotnet", "dotnet tool install --global", m.DotnetTools); ok {
		blocks = append(blocks, b)
	}

	// Linux system packages — guarded by manager presence; sudo may prompt once
	// on an interactive apply, and every line is `|| true` so a missing package
	// never aborts the script.
	if b, ok := installBlock("apt-get", "sudo apt-get install -y", m.AptPackages); ok {
		blocks = append(blocks, b)
	}
	if b, ok := installBlock("dnf", "sudo dnf install -y", m.DnfPackages); ok {
		blocks = append(blocks, b)
	}
	if b, ok := installBlock("pacman", "sudo pacman -S --noconfirm --needed", m.PacmanPackages); ok {
		blocks = append(blocks, b)
	}
	if b, ok := installBlock("snap", "sudo snap install", m.SnapPackages); ok {
		blocks = append(blocks, b)
	}
	if b, ok := installBlock("flatpak", "flatpak install -y flathub", m.FlatpakPackages); ok {
		blocks = append(blocks, b)
	}

	if len(blocks) == 0 {
		return "", false
	}

	// deno: the original module URL isn't recoverable from a bin name, so record
	// the names as a comment rather than emit a broken `deno install`.
	if len(m.DenoBins) > 0 {
		lines := []string{"# deno global bins (reinstall manually — original module URL not captured):"}
		for _, b := range m.DenoBins {
			lines = append(lines, "#   "+b)
		}
		blocks = append(blocks, strings.Join(lines, "\n"))
	}

	header := strings.Join([]string{
		"#!/bin/bash",
		"# Generated by `dothaven chezmoi-export`. chezmoi re-runs this on apply when it changes.",
		"set -uo pipefail",
	}, "\n")

	return fmt.Sprintf("%s\n\n%s\n\nexit 0\n", header, strings.Join(blocks, "\n\n")), true
}
