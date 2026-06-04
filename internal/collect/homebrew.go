package collect

import (
	"regexp"
	"sort"
	"strings"

	"github.com/doguyilmaz/dothaven/internal/snapshot"
)

// brewfileNoise matches progress/noise lines that `brew bundle dump` may emit on
// a cold cache. Everything else is kept, so first-class directives
// (go/npm/cargo/uv/whalebrew/vscode/mas/…) survive — an allowlist silently
// dropped go/npm and produced an incomplete "restorable" Brewfile.
var brewfileNoise = regexp.MustCompile(`^\s*(✔|✓|✗|⚠|ℹ|==>|Warning:|Error:)|JSON API`)

// ParseBrewList parses `brew list --formula` / `--cask` (one name per line):
// trims, drops blanks, sorts.
func ParseBrewList(text string) []string {
	out := []string{}
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		if l := strings.TrimSpace(line); l != "" {
			out = append(out, l)
		}
	}
	sort.Strings(out)
	return out
}

// ParseBrewfile cleans `brew bundle dump` stdout into a restorable Brewfile:
// drops noise lines, keeps every directive, trims surrounding blank lines.
func ParseBrewfile(text string) string {
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		if !brewfileNoise.MatchString(line) {
			kept = append(kept, line)
		}
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

// HomebrewCollector runs `brew list`/`brew bundle dump` and emits the installed
// formulae, casks, and a restorable Brewfile.
func HomebrewCollector(c Ctx) snapshot.Snapshot {
	out := snapshot.Snapshot{}

	formulaeOut, _ := c.Env.Run(c.Context, "brew", "list", "--formula")
	if formulae := ParseBrewList(formulaeOut); len(formulae) > 0 {
		out["apps.brew.formulae"] = snapshot.Section{Items: toItems(formulae)}
	}

	casksOut, _ := c.Env.Run(c.Context, "brew", "list", "--cask")
	if casks := ParseBrewList(casksOut); len(casks) > 0 {
		out["apps.brew.casks"] = snapshot.Section{Items: toItems(casks)}
	}

	bundleOut, _ := c.Env.Run(c.Context, "brew", "bundle", "dump", "--file=-")
	if bundle := ParseBrewfile(bundleOut); bundle != "" {
		out["apps.brew.bundle"] = snapshot.Section{Content: ptr(bundle)}
	}

	return out
}
