package collect

import (
	"runtime"
	"sort"
	"strings"

	"github.com/doguyilmaz/dothaven/internal/snapshot"
)

// parseNameLines reads one bare package name per line (apt-mark showmanual,
// pacman -Qqe, dnf repoquery --userinstalled --qf %{name}, flatpak app ids).
func parseNameLines(text string) []string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "#") {
			out = append(out, l)
		}
	}
	sort.Strings(out)
	return out
}

// ParseSnapList parses `snap list`: a table whose first row is a header; take
// the first column (the snap name) of every other row.
func ParseSnapList(text string) []string {
	var out []string
	for i, l := range strings.Split(text, "\n") {
		f := strings.Fields(l)
		if len(f) == 0 {
			continue
		}
		if i == 0 && f[0] == "Name" {
			continue
		}
		out = append(out, f[0])
	}
	sort.Strings(out)
	return out
}

// LinuxPackagesCollector inventories explicitly-installed system packages on
// Linux (apt/dnf/pacman) plus snap and flatpak apps. A no-op on other OSes —
// the package set is reinstalled by the generated install script on apply.
func LinuxPackagesCollector(c Ctx) snapshot.Snapshot {
	out := snapshot.Snapshot{}
	if runtime.GOOS != "linux" {
		return out
	}
	emit := func(id string, names []string) {
		if len(names) > 0 {
			out[id] = snapshot.Section{Items: toItems(names)}
		}
	}
	if s, _ := c.Env.Run(c.Context, "apt-mark", "showmanual"); s != "" {
		emit("packages.apt", parseNameLines(s))
	}
	if s, _ := c.Env.Run(c.Context, "dnf", "repoquery", "--userinstalled", "--qf", "%{name}"); s != "" {
		emit("packages.dnf", parseNameLines(s))
	}
	if s, _ := c.Env.Run(c.Context, "pacman", "-Qqe"); s != "" {
		emit("packages.pacman", parseNameLines(s))
	}
	if s, _ := c.Env.Run(c.Context, "snap", "list"); s != "" {
		emit("packages.snap", ParseSnapList(s))
	}
	if s, _ := c.Env.Run(c.Context, "flatpak", "list", "--app", "--columns=application"); s != "" {
		emit("packages.flatpak", parseNameLines(s))
	}
	return out
}
