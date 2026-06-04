package collect

import (
	"sort"
	"strings"

	"github.com/doguyilmaz/dothaven/internal/snapshot"
)

const (
	raycastPlist = "/Applications/Raycast.app/Contents/Info.plist"
	altTabPlist  = "/Applications/AltTab.app/Contents/Info.plist"
)

// ParseAppList parses `ls /Applications` output (one entry per line): trims each
// line, drops blanks, and returns the sorted set.
func ParseAppList(text string) []string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if t := strings.TrimSpace(l); t != "" {
			out = append(out, t)
		}
	}
	sort.Strings(out)
	return out
}

// AppsCollector gathers the macOS application inventory.
func AppsCollector(c Ctx) snapshot.Snapshot {
	out := snapshot.Snapshot{}

	out["apps.raycast"] = snapshot.Section{
		Pairs: map[string]string{"installed": appsInstalled(c.Env.Exists(raycastPlist))},
	}

	altInstalled := c.Env.Exists(altTabPlist)
	altPairs := map[string]string{"installed": appsInstalled(altInstalled)}
	if altInstalled {
		prefs, _ := c.Env.Run(c.Context, "defaults", "read", "com.lwouis.alt-tab-macos")
		if strings.TrimSpace(prefs) != "" {
			altPairs["preferences"] = "exists"
		}
	}
	out["apps.alttab"] = snapshot.Section{Pairs: altPairs}

	listing, _ := c.Env.Run(c.Context, "ls", "/Applications")
	if apps := ParseAppList(listing); len(apps) > 0 {
		out["apps.macos"] = snapshot.Section{Items: toItems(apps)}
	}

	return out
}

func appsInstalled(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
