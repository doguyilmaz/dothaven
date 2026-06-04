package collect

import (
	"sort"
	"strings"

	"github.com/doguyilmaz/dothaven/internal/snapshot"
)

// ParseExtensions parses `code --list-extensions` / `cursor --list-extensions`
// output (one extension id per line): trims, drops blanks, and sorts.
func ParseExtensions(text string) []string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			out = append(out, l)
		}
	}
	sort.Strings(out)
	return out
}

// EditorsExtCollector lists installed VS Code and Cursor extensions.
func EditorsExtCollector(c Ctx) snapshot.Snapshot {
	out := snapshot.Snapshot{}

	editors := []struct {
		cmd     string
		section string
	}{
		{"code", "editor.vscode.extensions"},
		{"cursor", "editor.cursor.extensions"},
	}
	for _, e := range editors {
		stdout, _ := c.Env.Run(c.Context, e.cmd, "--list-extensions")
		exts := ParseExtensions(stdout)
		if len(exts) > 0 {
			out[e.section] = snapshot.Section{Items: toItems(exts)}
		}
	}

	return out
}
