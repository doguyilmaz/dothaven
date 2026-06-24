package collect

import (
	"sort"
	"strings"

	"github.com/doguyilmaz/dothaven/internal/snapshot"
	"github.com/doguyilmaz/dothaven/internal/sys"
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

// vmDirNoise are non-version entries that appear alongside version dirs.
var vmDirNoise = map[string]bool{"current": true, "latest": true, "default": true}

// nestedVersions reads managers that store <base>/<tool>/<version> on disk
// (sdkman candidates, proto tools). The CLIs are shell functions, so the
// filesystem is the reliable source. Returns sorted (tool, version) pairs.
func nestedVersions(env sys.Env, base string) []ToolVersion {
	tools, err := env.ListDir(base)
	if err != nil {
		return nil
	}
	sort.Strings(tools)
	var out []ToolVersion
	for _, tool := range tools {
		if strings.HasPrefix(tool, ".") {
			continue
		}
		vers, err := env.ListDir(base + "/" + tool)
		if err != nil {
			continue
		}
		sort.Strings(vers)
		for _, v := range vers {
			if vmDirNoise[v] || strings.HasPrefix(v, ".") {
				continue
			}
			out = append(out, ToolVersion{Tool: tool, Version: v})
		}
	}
	return out
}

// flatVersions reads managers that store one version dir per entry (jenv, fvm).
func flatVersions(env sys.Env, dir string) []string {
	names, err := env.ListDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, n := range names {
		if vmDirNoise[n] || strings.HasPrefix(n, ".") {
			continue
		}
		out = append(out, n)
	}
	sort.Strings(out)
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

	// goenv / nodenv share pyenv/rbenv's CLI shape.
	for _, m := range []struct{ tool, id string }{
		{"goenv", "vm.goenv.versions"},
		{"nodenv", "vm.nodenv.versions"},
	} {
		if s, _ := c.Env.Run(c.Context, m.tool, "versions", "--bare"); true {
			if vs := ParseVersionLines(s); len(vs) > 0 {
				out[m.id] = snapshot.Section{Items: toItems(vs)}
			}
		}
	}

	home := c.Home
	if home == "" {
		home = c.Env.Home()
	}
	// Filesystem-backed managers (their CLIs are shell functions / heavy).
	if tvs := nestedVersions(c.Env, home+"/.sdkman/candidates"); len(tvs) > 0 {
		out["vm.sdkman.versions"] = snapshot.Section{Items: toolVersionItems(tvs)} // JVM family
	}
	if tvs := nestedVersions(c.Env, home+"/.proto/tools"); len(tvs) > 0 {
		out["vm.proto.versions"] = snapshot.Section{Items: toolVersionItems(tvs)}
	}
	if vs := flatVersions(c.Env, home+"/.jenv/versions"); len(vs) > 0 {
		out["vm.jenv.versions"] = snapshot.Section{Items: toItems(vs)} // Java
	}
	if vs := flatVersions(c.Env, home+"/.fvm/versions"); len(vs) > 0 {
		out["vm.fvm.versions"] = snapshot.Section{Items: toItems(vs)} // Flutter
	}
	return out
}
