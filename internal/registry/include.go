package registry

import (
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// ExtraCategory is the category of paths the user added themselves. Their
// backups live under ExtraCategory/ and map back to the same place under $HOME.
const ExtraCategory = "extra"

// Includes is the user's own list, on top of the registry: paths to carry
// (Paths) and paths they reviewed and declined (Declined), so the backup stops
// asking about them.
//
// The registry can never be complete, because every developer has a tool
// nobody else uses. A backup that only knows its built-in list silently leaves
// out the config that is hardest to rebuild. This is the escape hatch that
// needs no code change.
type Includes struct {
	Paths    []string // "~/"-relative, e.g. "~/.config/raycast"
	Declined []string
}

// ParseIncludes reads the include file: one path per line, "#" comments, a
// leading "!" marks a declined path. Paths are normalised to "~/rel"; anything
// outside home, or trying to climb out of it, is dropped. Restore maps these
// back under $HOME and must never be pointed anywhere else.
func ParseIncludes(text, home string) Includes {
	var inc Includes
	seen := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		if i := strings.Index(line, " #"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		declined := strings.HasPrefix(line, "!")
		p, ok := NormalizeInclude(strings.TrimPrefix(line, "!"), home)
		if !ok || seen[p] {
			continue
		}
		seen[p] = true
		if declined {
			inc.Declined = append(inc.Declined, p)
		} else {
			inc.Paths = append(inc.Paths, p)
		}
	}
	return inc
}

// NormalizeInclude turns "~/x", "$HOME/x" or an absolute path under home into
// "~/x". It reports false for anything outside home, for home itself, and for
// relative paths (which would depend on where the command was run).
func NormalizeInclude(p, home string) (string, bool) {
	p = strings.TrimSpace(p)
	switch {
	case p == "~" || p == "":
		return "", false
	case strings.HasPrefix(p, "~/"):
		p = filepath.Join(home, p[2:])
	case strings.HasPrefix(p, "$HOME/"):
		p = filepath.Join(home, p[6:])
	case !filepath.IsAbs(p):
		return "", false
	}
	rel, err := filepath.Rel(home, filepath.Clean(p))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return "~/" + filepath.ToSlash(rel), true
}

// FormatIncludes renders the include file, sorted, with a header saying what
// it is: it sits in ~/.config and will be found by someone who did not write it.
func FormatIncludes(inc Includes) string {
	var b strings.Builder
	b.WriteString("# dothaven: extra paths to back up, one per line, under your home folder.\n")
	b.WriteString("# A line starting with ! was reviewed and left out; backup won't ask again.\n")
	b.WriteString("# Edit freely, or use: dothaven include <path> / dothaven include --remove <path>\n")
	paths := append([]string(nil), inc.Paths...)
	sort.Strings(paths)
	for _, p := range paths {
		b.WriteString(p + "\n")
	}
	declined := append([]string(nil), inc.Declined...)
	sort.Strings(declined)
	for _, p := range declined {
		b.WriteString("!" + p + "\n")
	}
	return b.String()
}

// IncludeEntries projects the user's paths onto registry entries so backup,
// restore and export treat them like any other source. isDir is injected (the
// kind is decided by what is on disk now).
//
// They are Medium: scanned and redacted like everything else in a plaintext
// backup, written owner-only on restore. A path that sits inside a
// high-sensitivity entry is still protected: the backup gate checks every
// file against the high-sensitivity roots, whichever entry reached it.
func IncludeEntries(paths []string, isDir func(string) bool, home string) []Entry {
	out := make([]Entry, 0, len(paths))
	for _, p := range paths {
		rel := strings.TrimPrefix(p, "~/")
		kind := File
		if isDir(filepath.Join(home, filepath.FromSlash(rel))) {
			kind = Dir
		}
		out = append(out, Entry{
			ID:          "extra:" + p,
			Name:        p,
			Category:    ExtraCategory,
			Kind:        kind,
			Paths:       map[string]string{runtime.GOOS: p},
			BackupDest:  ExtraCategory + "/" + rel,
			Sensitivity: Medium,
		})
	}
	return out
}

// GitReferences lists the files and folders a git config points at inside
// home: hooks (core.hooksPath), ignore and attributes files, the commit
// template, the init template folder, and the files pulled in by [include]
// and [includeIf] (a work identity, a signing key's config), which are the
// ones people forget. Restoring a .gitconfig without them silently disables hooks
// or signs commits as the wrong person. dir is the config file's folder, which
// a relative include path is relative to. Returns "~/"-relative paths.
func GitReferences(config, dir, home string) []string {
	keys := map[string]map[string]bool{
		"core":      {"hookspath": true, "excludesfile": true, "attributesfile": true},
		"commit":    {"template": true},
		"init":      {"templatedir": true},
		"include":   {"path": true},
		"includeif": {"path": true},
	}
	section := ""
	seen := map[string]bool{}
	var out []string
	for _, line := range strings.Split(config, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if line[0] == '[' {
			name := strings.Trim(line, "[]")
			if i := strings.IndexAny(name, " \t\""); i >= 0 {
				name = name[:i] // [includeIf "gitdir:~/work/"] → includeif
			}
			section = strings.ToLower(name)
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || !keys[section][strings.ToLower(strings.TrimSpace(k))] {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"`)
		switch {
		case strings.HasPrefix(v, "~/"):
			v = filepath.Join(home, v[2:])
		case strings.HasPrefix(v, "$HOME/"):
			v = filepath.Join(home, v[6:])
		case v != "" && !filepath.IsAbs(v):
			v = filepath.Join(dir, v)
		}
		rel, err := filepath.Rel(home, filepath.Clean(v))
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			continue // outside home: not something a restore can put back
		}
		p := "~/" + filepath.ToSlash(rel)
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}
