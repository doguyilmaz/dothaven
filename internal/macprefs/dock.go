package macprefs

import (
	"encoding/xml"
	"net/url"
	"regexp"
	"strings"
)

// dockURL matches one app's location in `defaults read com.apple.dock
// persistent-apps`, which prints old-style plist text.
var dockURL = regexp.MustCompile(`"_CFURLString"\s*=\s*"([^"]+)"`)

// ParseDockApps returns the app bundles pinned in the Dock, in order. The Dock
// layout is a nested array the per-key capture cannot replay, and it is the
// first thing anyone looks at on a new Mac — so it gets a path of its own.
func ParseDockApps(out string) []string {
	var apps []string
	seen := map[string]bool{}
	for _, m := range dockURL.FindAllStringSubmatch(out, -1) {
		p := m[1]
		if strings.HasPrefix(p, "file://") {
			u, err := url.Parse(p)
			if err != nil {
				continue
			}
			p = u.Path
		}
		p = strings.TrimSuffix(p, "/")
		if !strings.HasSuffix(p, ".app") || seen[p] {
			continue
		}
		seen[p] = true
		apps = append(apps, p)
	}
	return apps
}

// DockTile is the plist fragment `defaults write com.apple.dock persistent-apps
// -array-add` takes for one app: a file tile pointing at its bundle.
func DockTile(appPath string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte("file://"+(&url.URL{Path: appPath + "/"}).EscapedPath()))
	return "<dict><key>tile-data</key><dict><key>file-data</key><dict>" +
		"<key>_CFURLString</key><string>" + b.String() + "</string>" +
		"<key>_CFURLStringType</key><integer>15</integer>" +
		"</dict></dict></dict>"
}
