package collect

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/doguyilmaz/dothaven/internal/registry"
	"github.com/doguyilmaz/dothaven/internal/snapshot"
)

// dotfilesSweepNoise lists ephemeral, always-regenerated entries safe to ignore.
// Kept deliberately small: anything not clearly noise lands in "review" so
// nothing important is hidden.
var dotfilesSweepNoise = map[string]bool{
	".DS_Store":                 true,
	".CFUserTextEncoding":       true,
	".localized":                true,
	".Trash":                    true,
	".cache":                    true,
	".lesshst":                  true,
	".node_repl_history":        true,
	".bash_history":             true,
	".zsh_history":              true,
	".zsh_sessions":             true,
	".zcompdump":                true,
	".cups":                     true,
	".wget-hsts":                true,
	".sudo_as_admin_successful": true,
}

var dotfilesSweepTopRe = regexp.MustCompile(`^~/(\.[^/]+)`)

var dotfilesConfigRe = regexp.MustCompile(`^~/\.config/([^/]+)`)

// dotfilesConfigNoise are ephemeral entries to ignore inside ~/.config.
var dotfilesConfigNoise = map[string]bool{".DS_Store": true, ".git": true}

// DotfilesSweep holds the classification of home dotfiles into managed (covered
// by the registry) and review (unknown, not noise) buckets.
type DotfilesSweep struct {
	Managed []string
	Review  []string
}

// ManagedDotNames derives the set of top-level ~/.X names already covered by the
// registry (the single source of truth). It mirrors the TS by reading each
// entry's darwin path, falling back to linux, then taking the first segment
// after ~/.
func ManagedDotNames(entries []registry.Entry) map[string]bool {
	set := map[string]bool{}
	for _, e := range entries {
		p := e.Paths["darwin"]
		if p == "" {
			p = e.Paths["linux"]
		}
		if p == "" {
			continue
		}
		if m := dotfilesSweepTopRe.FindStringSubmatch(p); m != nil {
			set[m[1]] = true
		}
	}
	return set
}

// ManagedConfigNames derives the set of ~/.config/<name> entries covered by the
// registry, across both darwin and linux paths (a tool's ~/.config form may
// appear only on linux). Used to flag the ~/.config children that aren't
// covered. Otherwise the top-level sweep marks all of ~/.config "managed" and
// silently hides every uncovered tool living under it.
func ManagedConfigNames(entries []registry.Entry) map[string]bool {
	set := map[string]bool{}
	for _, e := range entries {
		for _, goos := range []string{"darwin", "linux"} {
			if m := dotfilesConfigRe.FindStringSubmatch(e.Paths[goos]); m != nil {
				set[m[1]] = true
			}
		}
	}
	return set
}

// ParseLsA parses `ls -A` output into trimmed, non-empty entry names.
func ParseLsA(text string) []string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// ClassifyDotfiles buckets dot entries into managed/review, dropping noise and
// non-dot entries. Entries are sorted before classification (matching TS).
func ClassifyDotfiles(entries []string, managed, noise map[string]bool) DotfilesSweep {
	result := DotfilesSweep{Managed: []string{}, Review: []string{}}

	filtered := make([]string, 0, len(entries))
	for _, name := range entries {
		if strings.HasPrefix(name, ".") && name != "." && name != ".." {
			filtered = append(filtered, name)
		}
	}
	sort.Strings(filtered)

	for _, name := range filtered {
		switch {
		case managed[name]:
			result.Managed = append(result.Managed, name)
		case !noise[name]:
			result.Review = append(result.Review, name)
		}
	}
	return result
}

// ClassifyConfigEntries buckets ~/.config children into managed/review. Unlike
// ClassifyDotfiles these names are not dot-prefixed; "." and ".." are dropped.
func ClassifyConfigEntries(entries []string, managed, noise map[string]bool) DotfilesSweep {
	result := DotfilesSweep{Managed: []string{}, Review: []string{}}
	names := make([]string, 0, len(entries))
	for _, n := range entries {
		if n != "" && n != "." && n != ".." {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		switch {
		case managed[name]:
			result.Managed = append(result.Managed, name)
		case !noise[name]:
			result.Review = append(result.Review, name)
		}
	}
	return result
}

// DotfilesSweepCollector runs `ls -A ~` and classifies the home dotfiles against
// the registry, emitting "home.dotfiles.review" and "home.dotfiles.managed"
// sections (each only when non-empty).
func DotfilesSweepCollector(c Ctx) snapshot.Snapshot {
	out := snapshot.Snapshot{}

	home := c.Home
	if home == "" {
		home = filepath.Clean(c.Env.Home())
	}

	stdout, _ := c.Env.Run(c.Context, "ls", "-A", home)
	entries := ParseLsA(stdout)
	if len(entries) == 0 {
		return out
	}

	sweep := ClassifyDotfiles(entries, ManagedDotNames(registry.Entries), dotfilesSweepNoise)
	if len(sweep.Review) > 0 {
		out["home.dotfiles.review"] = snapshot.Section{Items: toItems(sweep.Review)}
	}
	if len(sweep.Managed) > 0 {
		out["home.dotfiles.managed"] = snapshot.Section{Items: toItems(sweep.Managed)}
	}

	// The top-level sweep marks all of ~/.config "managed" because registry
	// entries register only their first segment (.config). Sweep one level
	// deeper so uncovered ~/.config/<tool> configs (sheldon, powershell, …)
	// surface for review instead of being silently dropped.
	cfgOut, _ := c.Env.Run(c.Context, "ls", "-A", home+"/.config")
	if cfgEntries := ParseLsA(cfgOut); len(cfgEntries) > 0 {
		cfg := ClassifyConfigEntries(cfgEntries, ManagedConfigNames(registry.Entries), dotfilesConfigNoise)
		if len(cfg.Review) > 0 {
			out["home.config.review"] = snapshot.Section{Items: toItems(cfg.Review)}
		}
	}
	return out
}

// uncoveredNoise are home entries that are state, caches or toolchains rather
// than config: offering them for backup would bury the real candidates. None
// of it is lost by leaving it out: it rebuilds, or it is history.
var uncoveredNoise = map[string]bool{
	".local": true, ".npm": true, ".cargo": true, ".rustup": true, ".nvm": true,
	".pyenv": true, ".rbenv": true, ".gem": true, ".gradle": true, ".m2": true,
	".cocoapods": true, ".pub-cache": true, ".dartServer": true, ".bun": true,
	".deno": true, ".vscode": true, ".vscode-server": true, ".cursor-server": true,
	".pnpm-store": true, ".yarn": true, ".cpan": true, ".conda": true, ".julia": true,
	".swiftpm": true, ".expo": true, ".android": true, ".oh-my-zsh": true,
	".zsh_history": true, ".bash_history": true, ".python_history": true,
	".psql_history": true, ".mysql_history": true, ".sqlite_history": true,
	".viminfo": true, ".lesshst": true, ".node_repl_history": true, ".irb_history": true,
	".zcompdump": true, ".zsh_sessions": true, ".Trash": true, ".cache": true,
	".DS_Store": true, ".CFUserTextEncoding": true, ".localized": true, ".cups": true,
	".wget-hsts": true, ".sudo_as_admin_successful": true, ".bash_sessions": true,
	".dbus": true, ".pki": true, ".xsession-errors": true, ".ICEauthority": true,
	".Xauthority": true, ".dotnet": true, ".nuget": true, ".sdkman": true, ".jenv": true,
	".volta": true, ".fnm": true, ".asdf": true, ".proto": true, ".fvm": true,
	".docker": true, ".minikube": true, ".colima": true, ".lima": true, ".orbstack": true,
	".ollama": true, ".lmstudio": true, ".matplotlib": true, ".ipython": true, ".keras": true,
	".vim": true, ".emacs.d": true, ".tmp": true,
}

var claudeNoise = map[string]bool{
	"projects": true, "todos": true, "shell-snapshots": true, "statsig": true, "ide": true,
	"debug": true, "file-history": true, "session-env": true, "history.jsonl": true,
	"cache": true, "logs": true, "telemetry": true, ".credentials.json": true, "local": true,
	"downloads": true, "paste-cache": true, "stats-cache.json": true, "__store.db": true,
	".DS_Store": true, "sessions": true, "tasks": true, "backups": true, ".last-cleanup": true,
	"policy-limits.json": true, "policy-limits.json.stamp.json": true, "remote-settings.json": true,
	"environment-manager": true, "launcher-settings.json": true, "plans": true,
}

// Codex and Gemini keep sessions, history and caches beside their config;
// what is left once those are skipped is worth asking about.
var codexNoise = map[string]bool{
	"sessions": true, "archived_sessions": true, "history.jsonl": true, "log": true,
	"cache": true, "tmp": true, "shell_snapshots": true, "version.json": true,
	"models_cache.json": true, "internal_storage.json": true, ".DS_Store": true,
}

var geminiNoise = map[string]bool{
	"tmp": true, "history": true, "installation_id": true, "user_id": true,
	"google_accounts.json": true, "google_account_id": true, ".DS_Store": true,
}

// configNoise under ~/.config: browser profiles on Linux (gigabytes of cache
// and history, synced by the browser's own account) are not config to carry.
var configNoise = map[string]bool{
	".DS_Store": true, ".git": true, "dothaven": true,
	"chromium": true, "google-chrome": true, "google-chrome-beta": true, "BraveSoftware": true,
	"microsoft-edge": true, "vivaldi": true, "opera": true, "pulse": true,
}

// Uncovered lists paths under home that look like config and that neither the
// registry nor the user's includes cover, as "~/"-relative paths. It looks one
// level into ~, ~/.config, ~/.claude, ~/.codex and ~/.gemini, which is where
// nearly all of it lives.
//
// This is how a backup stops being limited to what dothaven already knows
// about: whatever it does not recognise is put in front of the user once,
// instead of being left behind without a word.
func Uncovered(listDir func(string) ([]string, error), home string, entries []registry.Entry, inc registry.Includes) []string {
	covered := map[string]bool{}
	for _, e := range entries {
		for _, goos := range []string{"darwin", "linux"} {
			if p := e.Paths[goos]; strings.HasPrefix(p, "~/") {
				covered[p] = true
			}
		}
	}
	for _, p := range append(append([]string(nil), inc.Paths...), inc.Declined...) {
		covered[p] = true
	}
	// isCovered: the path itself, one of its parents, or (for a directory
	// the registry reaches into) any child is tracked.
	isCovered := func(p string) bool {
		for c := range covered {
			if c == p || strings.HasPrefix(p, c+"/") {
				return true
			}
		}
		return false
	}
	reachesInto := func(p string) bool {
		for c := range covered {
			if strings.HasPrefix(c, p+"/") {
				return true
			}
		}
		return false
	}

	var out []string
	sweep := func(dir, prefix string, noise map[string]bool, dotOnly bool) {
		names, err := listDir(filepath.Join(home, dir))
		if err != nil {
			return
		}
		sort.Strings(names)
		for _, n := range names {
			if noise[n] || (dotOnly && !strings.HasPrefix(n, ".")) {
				continue
			}
			p := prefix + n
			if isCovered(p) || reachesInto(p) {
				continue
			}
			out = append(out, p)
		}
	}
	sweep("", "~/", uncoveredNoise, true)
	sweep(".config", "~/.config/", configNoise, false)
	sweep(".claude", "~/.claude/", claudeNoise, false)
	sweep(".codex", "~/.codex/", codexNoise, false)
	sweep(".gemini", "~/.gemini/", geminiNoise, false)
	// Personal scripts are easy to forget when moving machines.
	if names, err := listDir(filepath.Join(home, "bin")); err == nil && len(names) > 0 && !isCovered("~/bin") {
		out = append(out, "~/bin")
	}
	return out
}
