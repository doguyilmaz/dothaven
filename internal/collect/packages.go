package collect

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"github.com/doguyilmaz/dothaven/internal/snapshot"
)

// PkgItem is a parsed package name + version.
type PkgItem struct {
	Name    string
	Version string
}

// NodeVersion is a parsed fnm entry.
type NodeVersion struct {
	Version   string
	IsDefault bool
}

var bunTreeLine = regexp.MustCompile(`^\s*[├└]──\s*(.+?)\s*$`)
var fnmStar = regexp.MustCompile(`^\s*\*`)
var fnmSplit = regexp.MustCompile(`[\s,]+`)
var fnmDefault = regexp.MustCompile(`\bdefault\b`)

// splitSpec splits a package spec into name + version, preserving scoped names
// (@scope/pkg@1.2.3). Mirrors the TS lastIndexOf("@") rule.
func splitSpec(spec string) PkgItem {
	at := strings.LastIndex(spec, "@")
	if at <= 0 {
		return PkgItem{Name: spec, Version: ""}
	}
	return PkgItem{Name: spec[:at], Version: spec[at+1:]}
}

func pkgSortByName(pkgs []PkgItem) {
	sort.SliceStable(pkgs, func(i, j int) bool { return pkgs[i].Name < pkgs[j].Name })
}

func pkgsFromDeps(deps map[string]json.RawMessage) []PkgItem {
	out := make([]PkgItem, 0, len(deps))
	for name, raw := range deps {
		if name == "" {
			continue
		}
		var info struct {
			Version string `json:"version"`
		}
		_ = json.Unmarshal(raw, &info)
		out = append(out, PkgItem{Name: name, Version: info.Version})
	}
	pkgSortByName(out)
	return out
}

// ParseNpmGlobal parses `npm ls -g --depth=0 --json`.
func ParseNpmGlobal(jsonText string) []PkgItem {
	var data struct {
		Dependencies map[string]json.RawMessage `json:"dependencies"`
	}
	if err := json.Unmarshal([]byte(jsonText), &data); err != nil {
		return []PkgItem{}
	}
	return pkgsFromDeps(data.Dependencies)
}

// ParsePnpmGlobal parses `pnpm ls -g --depth=0 --json` (array or object form).
func ParsePnpmGlobal(jsonText string) []PkgItem {
	trimmed := strings.TrimSpace(jsonText)
	if trimmed == "" {
		return []PkgItem{}
	}
	type node struct {
		Dependencies map[string]json.RawMessage `json:"dependencies"`
	}
	if strings.HasPrefix(trimmed, "[") {
		var arr []node
		if err := json.Unmarshal([]byte(trimmed), &arr); err != nil || len(arr) == 0 {
			return []PkgItem{}
		}
		return pkgsFromDeps(arr[0].Dependencies)
	}
	var obj node
	if err := json.Unmarshal([]byte(trimmed), &obj); err != nil {
		return []PkgItem{}
	}
	return pkgsFromDeps(obj.Dependencies)
}

// ParseBunGlobal parses `bun pm ls -g` tree output (header line + `├──`/`└──` rows).
func ParseBunGlobal(text string) []PkgItem {
	out := []PkgItem{}
	for _, line := range strings.Split(text, "\n") {
		m := bunTreeLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		p := splitSpec(m[1])
		if p.Name == "" {
			continue
		}
		out = append(out, p)
	}
	pkgSortByName(out)
	return out
}

// ParseFnmList parses `fnm ls` output (`* v20.20.2`, `* v24.16.0 default`, `* system`).
func ParseFnmList(text string) []NodeVersion {
	out := []NodeVersion{}
	for _, line := range strings.Split(text, "\n") {
		l := strings.TrimSpace(fnmStar.ReplaceAllString(line, ""))
		if l == "" {
			continue
		}
		tokens := fnmSplit.Split(l, -1)
		version := ""
		for _, t := range tokens {
			if t != "" {
				version = t
				break
			}
		}
		if version == "" {
			continue
		}
		out = append(out, NodeVersion{Version: version, IsDefault: fnmDefault.MatchString(l)})
	}
	return out
}

// ParsePipxList parses `pipx list --short` ("black 24.10.0" per line).
func ParsePipxList(text string) []PkgItem {
	out := []PkgItem{}
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		p := PkgItem{Name: f[0]}
		if len(f) > 1 {
			p.Version = f[1]
		}
		out = append(out, p)
	}
	pkgSortByName(out)
	return out
}

// ParseUvTool parses `uv tool list`: a non-indented "name vX.Y.Z" line per tool,
// with indented "- entrypoint" lines beneath that are ignored.
func ParseUvTool(text string) []PkgItem {
	out := []PkgItem{}
	for _, line := range strings.Split(text, "\n") {
		if line == "" || line[0] == ' ' || line[0] == '\t' || line[0] == '-' {
			continue
		}
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		p := PkgItem{Name: f[0]}
		if len(f) > 1 {
			p.Version = strings.TrimPrefix(f[1], "v")
		}
		out = append(out, p)
	}
	pkgSortByName(out)
	return out
}

// ParseComposerGlobal parses `composer global show --format=json`.
func ParseComposerGlobal(jsonText string) []PkgItem {
	var data struct {
		Installed []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"installed"`
	}
	if err := json.Unmarshal([]byte(jsonText), &data); err != nil {
		return []PkgItem{}
	}
	out := make([]PkgItem, 0, len(data.Installed))
	for _, p := range data.Installed {
		if p.Name != "" {
			out = append(out, PkgItem{Name: p.Name, Version: strings.TrimPrefix(p.Version, "v")})
		}
	}
	pkgSortByName(out)
	return out
}

// ParsePubGlobal parses `dart pub global list`: "pkg 1.2.3" (or "… from <path>").
func ParsePubGlobal(text string) []PkgItem {
	out := []PkgItem{}
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		p := PkgItem{Name: f[0]}
		if len(f) > 1 {
			p.Version = f[1]
		}
		out = append(out, p)
	}
	pkgSortByName(out)
	return out
}

// ParseDotnetTool parses `dotnet tool list --global`: a header + dashed separator
// then "PackageId  Version  Commands" rows.
func ParseDotnetTool(text string) []PkgItem {
	out := []PkgItem{}
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || f[0] == "Package" || strings.HasPrefix(f[0], "---") {
			continue
		}
		out = append(out, PkgItem{Name: f[0], Version: f[1]})
	}
	pkgSortByName(out)
	return out
}

func pkgItems(pkgs []PkgItem) []snapshot.Item {
	out := make([]snapshot.Item, 0, len(pkgs))
	for _, p := range pkgs {
		raw := p.Name
		cols := []string{p.Name}
		if p.Version != "" {
			raw = p.Name + "@" + p.Version
			cols = []string{p.Name, p.Version}
		}
		out = append(out, snapshot.Item{Raw: raw, Columns: cols})
	}
	return out
}

// PackagesCollector gathers globally-installed packages from npm, bun, pnpm,
// fnm node versions, and deno-installed binaries.
func PackagesCollector(c Ctx) snapshot.Snapshot {
	out := snapshot.Snapshot{}

	if s, _ := c.Env.Run(c.Context, "npm", "ls", "-g", "--depth=0", "--json"); true {
		if npm := ParseNpmGlobal(s); len(npm) > 0 {
			out["packages.npm.global"] = snapshot.Section{Items: pkgItems(npm)}
		}
	}

	if s, _ := c.Env.Run(c.Context, "bun", "pm", "ls", "-g"); true {
		if bun := ParseBunGlobal(s); len(bun) > 0 {
			out["packages.bun.global"] = snapshot.Section{Items: pkgItems(bun)}
		}
	}

	if s, _ := c.Env.Run(c.Context, "pnpm", "ls", "-g", "--depth=0", "--json"); true {
		if pnpm := ParsePnpmGlobal(s); len(pnpm) > 0 {
			out["packages.pnpm.global"] = snapshot.Section{Items: pkgItems(pnpm)}
		}
	}

	if s, _ := c.Env.Run(c.Context, "fnm", "ls"); true {
		if versions := ParseFnmList(s); len(versions) > 0 {
			items := make([]snapshot.Item, 0, len(versions))
			for _, v := range versions {
				if v.IsDefault {
					items = append(items, snapshot.Item{Raw: v.Version + " (default)", Columns: []string{v.Version, "default"}})
				} else {
					items = append(items, snapshot.Item{Raw: v.Version, Columns: []string{v.Version}})
				}
			}
			out["packages.node.fnm"] = snapshot.Section{Items: items}
		}
	}

	if bins, err := c.Env.ListDir(c.Home + "/.deno/bin"); err == nil && len(bins) > 0 {
		sorted := append([]string(nil), bins...)
		sort.Strings(sorted)
		out["packages.deno.bin"] = snapshot.Section{Items: toItems(sorted)}
	}

	if s, _ := c.Env.Run(c.Context, "pipx", "list", "--short"); true {
		if p := ParsePipxList(s); len(p) > 0 {
			out["packages.pipx"] = snapshot.Section{Items: pkgItems(p)}
		}
	}

	// `go install`ed binaries — user tools with no config file to reproduce them.
	if bins, err := c.Env.ListDir(c.Home + "/go/bin"); err == nil && len(bins) > 0 {
		sorted := append([]string(nil), bins...)
		sort.Strings(sorted)
		out["packages.go.bin"] = snapshot.Section{Items: toItems(sorted)}
	}

	if s, _ := c.Env.Run(c.Context, "uv", "tool", "list"); true {
		if p := ParseUvTool(s); len(p) > 0 {
			out["packages.uv"] = snapshot.Section{Items: pkgItems(p)}
		}
	}
	if s, _ := c.Env.Run(c.Context, "composer", "global", "show", "--format=json"); true {
		if p := ParseComposerGlobal(s); len(p) > 0 {
			out["packages.composer"] = snapshot.Section{Items: pkgItems(p)}
		}
	}
	if s, _ := c.Env.Run(c.Context, "dart", "pub", "global", "list"); true {
		if p := ParsePubGlobal(s); len(p) > 0 {
			out["packages.pub"] = snapshot.Section{Items: pkgItems(p)}
		}
	}
	if s, _ := c.Env.Run(c.Context, "dotnet", "tool", "list", "--global"); true {
		if p := ParseDotnetTool(s); len(p) > 0 {
			out["packages.dotnet"] = snapshot.Section{Items: pkgItems(p)}
		}
	}

	return out
}
