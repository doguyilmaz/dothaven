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
// covered — otherwise the top-level sweep marks all of ~/.config "managed" and
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
