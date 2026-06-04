package collect

import (
	"regexp"
	"sort"

	"github.com/doguyilmaz/dothaven/internal/snapshot"
)

var fontExtRe = regexp.MustCompile(`(?i)\.(ttf|ttc|otf|otc|dfont|woff2?|pfb)$`)

// FilterFonts keeps only font files from a directory listing and sorts them.
func FilterFonts(names []string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		if fontExtRe.MatchString(n) {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// fontsIn merges fonts found across several directories (missing dirs yield
// nothing), deduped and sorted.
func fontsIn(c Ctx, dirs []string) []string {
	seen := map[string]struct{}{}
	for _, dir := range dirs {
		names, err := c.Env.ListDir(dir)
		if err != nil {
			continue
		}
		for _, name := range FilterFonts(names) {
			seen[name] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// FontsCollector lists user and system installed fonts.
func FontsCollector(c Ctx) snapshot.Snapshot {
	out := snapshot.Snapshot{}

	user := fontsIn(c, []string{
		c.Home + "/Library/Fonts",      // macOS
		c.Home + "/.fonts",             // Linux
		c.Home + "/.local/share/fonts", // Linux
	})
	if len(user) > 0 {
		out["fonts.user"] = snapshot.Section{Items: toItems(user)}
	}

	system := fontsIn(c, []string{
		"/Library/Fonts",         // macOS
		"/usr/share/fonts",       // Linux
		"/usr/local/share/fonts", // Linux
	})
	if len(system) > 0 {
		out["fonts.system"] = snapshot.Section{Items: toItems(system)}
	}

	return out
}
