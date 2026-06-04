package collect

import (
	"strings"

	"github.com/doguyilmaz/dothaven/internal/snapshot"
)

// ToolVersion is one installed (tool, version) pair from a version manager.
type ToolVersion struct {
	Tool    string
	Version string
}

// ParseAsdfList parses `asdf list`: a non-indented tool name followed by
// indented version lines (a leading "*" marks the current one).
//
//	nodejs
//	  18.20.0
//	 *20.11.0
func ParseAsdfList(text string) []ToolVersion {
	var out []ToolVersion
	tool := ""
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if line[0] != ' ' && line[0] != '\t' {
			tool = strings.TrimSpace(line)
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(strings.TrimSpace(line), "*"))
		if tool == "" || len(fields) == 0 {
			continue
		}
		out = append(out, ToolVersion{Tool: tool, Version: fields[0]})
	}
	return out
}

// ParseVersionLines parses `pyenv versions --bare` / `rbenv versions --bare`:
// one version per line. A leading "*", trailing "(set by …)", and "system" are
// dropped.
func ParseVersionLines(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		v := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "*"))
		if i := strings.IndexByte(v, ' '); i >= 0 {
			v = v[:i]
		}
		if v != "" && v != "system" {
			out = append(out, v)
		}
	}
	return out
}

func toolVersionItems(tvs []ToolVersion) []snapshot.Item {
	out := make([]snapshot.Item, 0, len(tvs))
	for _, tv := range tvs {
		out = append(out, snapshot.Item{Raw: tv.Tool + " " + tv.Version, Columns: []string{tv.Tool, tv.Version}})
	}
	return out
}

// VersionManagersCollector inventories versions installed via asdf, pyenv, and
// rbenv. The declarative configs (.tool-versions, mise config) live in the
// registry; this captures what is actually installed for parity checks.
func VersionManagersCollector(c Ctx) snapshot.Snapshot {
	out := snapshot.Snapshot{}

	if s, _ := c.Env.Run(c.Context, "asdf", "list"); true {
		if tvs := ParseAsdfList(s); len(tvs) > 0 {
			out["vm.asdf.versions"] = snapshot.Section{Items: toolVersionItems(tvs)}
		}
	}
	if s, _ := c.Env.Run(c.Context, "pyenv", "versions", "--bare"); true {
		if vs := ParseVersionLines(s); len(vs) > 0 {
			out["vm.pyenv.versions"] = snapshot.Section{Items: toItems(vs)}
		}
	}
	if s, _ := c.Env.Run(c.Context, "rbenv", "versions", "--bare"); true {
		if vs := ParseVersionLines(s); len(vs) > 0 {
			out["vm.rbenv.versions"] = snapshot.Section{Items: toItems(vs)}
		}
	}
	return out
}
