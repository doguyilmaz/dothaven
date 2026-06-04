package collect

import (
	"regexp"
	"sort"
	"strings"

	"github.com/doguyilmaz/dothaven/internal/snapshot"
)

// GoInfo is the parsed `go version` output.
type GoInfo struct {
	Version  string
	Platform string
}

// RustToolchain is one entry from `rustup toolchain list`.
type RustToolchain struct {
	Name  string
	Flags string
}

// Crate is one entry from `cargo install --list`.
type Crate struct {
	Name    string
	Version string
}

// XcodeInfo is the parsed `xcodebuild -version` output.
type XcodeInfo struct {
	Version string
	Build   string
}

var (
	runtimesGoRe        = regexp.MustCompile(`^go version (\S+) (\S+)`)
	runtimesToolchainRe = regexp.MustCompile(`^(\S+)(?:\s+\(([^)]*)\))?$`)
	runtimesCrateRe     = regexp.MustCompile(`^(\S+)\s+v(\S+?)(?:\s+\([^)]*\))?:\s*$`)
	runtimesSwiftRe     = regexp.MustCompile(`Apple Swift version (\S+)`)
	runtimesXcodeVerRe  = regexp.MustCompile(`Xcode\s+(\S+)`)
	runtimesXcodeBldRe  = regexp.MustCompile(`Build version\s+(\S+)`)
	runtimesZigRe       = regexp.MustCompile(`^\d`)
	runtimesAdbRe       = regexp.MustCompile(`(?m)^Version\s+(\S+)`)
)

// ParseVersionToken extracts the token after a word: `rustc 1.96.0 (...)` with
// word "rustc" → "1.96.0".
func ParseVersionToken(text, word string) string {
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(word) + `\s+(\S+)`)
	m := re.FindStringSubmatch(text)
	if m == nil {
		return ""
	}
	return m[1]
}

// ParseGoVersion parses `go version go1.26.3 darwin/arm64`.
func ParseGoVersion(text string) *GoInfo {
	m := runtimesGoRe.FindStringSubmatch(strings.TrimSpace(text))
	if m == nil {
		return nil
	}
	return &GoInfo{Version: m[1], Platform: m[2]}
}

// ParseRustupToolchains parses `rustup toolchain list`
// (`stable-aarch64-apple-darwin (active, default)`).
func ParseRustupToolchains(text string) []RustToolchain {
	var out []RustToolchain
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		l := strings.TrimSpace(line)
		if l == "" {
			continue
		}
		var t RustToolchain
		if m := runtimesToolchainRe.FindStringSubmatch(l); m != nil {
			t = RustToolchain{Name: m[1], Flags: m[2]}
		} else {
			t = RustToolchain{Name: l}
		}
		if strings.Contains(t.Name, " ") {
			continue
		}
		out = append(out, t)
	}
	return out
}

// ParseCargoCrates parses `cargo install --list` (`ripgrep v14.1.0:` followed by
// indented binaries; git/path installs add a ` (source)` before the colon).
func ParseCargoCrates(text string) []Crate {
	var out []Crate
	for _, line := range strings.Split(text, "\n") {
		if m := runtimesCrateRe.FindStringSubmatch(line); m != nil {
			out = append(out, Crate{Name: m[1], Version: m[2]})
		}
	}
	return out
}

// ParseSwiftVersion parses `swift --version` (`... Apple Swift version 6.3.1 (...)`).
func ParseSwiftVersion(text string) string {
	m := runtimesSwiftRe.FindStringSubmatch(text)
	if m == nil {
		return ""
	}
	return m[1]
}

// ParseXcodeVersion parses `xcodebuild -version`
// (`Xcode 26.4.1` / `Build version 17E202`).
func ParseXcodeVersion(text string) *XcodeInfo {
	v := runtimesXcodeVerRe.FindStringSubmatch(text)
	if v == nil {
		return nil
	}
	info := &XcodeInfo{Version: v[1]}
	if b := runtimesXcodeBldRe.FindStringSubmatch(text); b != nil {
		info.Build = b[1]
	}
	return info
}

// ParseZigVersion parses `zig version` (single line like `0.13.0`).
func ParseZigVersion(text string) string {
	first := ""
	if lines := strings.Split(strings.TrimSpace(text), "\n"); len(lines) > 0 {
		first = strings.TrimSpace(lines[0])
	}
	if runtimesZigRe.MatchString(first) {
		return first
	}
	return ""
}

// ParseAdbVersion parses `adb version` — the `Version 36.0.2-...` line
// (platform-tools version).
func ParseAdbVersion(text string) string {
	m := runtimesAdbRe.FindStringSubmatch(text)
	if m == nil {
		return ""
	}
	return m[1]
}

// RuntimesCollector inventories language/SDK toolchains: go, rust, swift, zig,
// xcode, and android.
func RuntimesCollector(c Ctx) snapshot.Snapshot {
	out := snapshot.Snapshot{}

	if outStr, _ := c.Env.Run(c.Context, "go", "version"); true {
		if g := ParseGoVersion(outStr); g != nil {
			out["runtimes.go"] = snapshot.Section{Pairs: map[string]string{"version": g.Version, "platform": g.Platform}}
		}
	}

	{
		pairs := map[string]string{}
		if rustcOut, _ := c.Env.Run(c.Context, "rustc", "--version"); true {
			if v := ParseVersionToken(rustcOut, "rustc"); v != "" {
				pairs["rustc"] = v
			}
		}
		if cargoOut, _ := c.Env.Run(c.Context, "cargo", "--version"); true {
			if v := ParseVersionToken(cargoOut, "cargo"); v != "" {
				pairs["cargo"] = v
			}
		}
		if len(pairs) > 0 {
			out["runtimes.rust"] = snapshot.Section{Pairs: pairs}
		}
	}

	if tcOut, _ := c.Env.Run(c.Context, "rustup", "toolchain", "list"); true {
		toolchains := ParseRustupToolchains(tcOut)
		if len(toolchains) > 0 {
			items := make([]snapshot.Item, len(toolchains))
			for i, t := range toolchains {
				cols := []string{t.Name}
				raw := t.Name
				if t.Flags != "" {
					cols = append(cols, t.Flags)
					raw = t.Name + " (" + t.Flags + ")"
				}
				items[i] = snapshot.Item{Raw: raw, Columns: cols}
			}
			out["runtimes.rust.toolchains"] = snapshot.Section{Items: items}
		}
	}

	if crateOut, _ := c.Env.Run(c.Context, "cargo", "install", "--list"); true {
		crates := ParseCargoCrates(crateOut)
		if len(crates) > 0 {
			items := make([]snapshot.Item, len(crates))
			for i, cr := range crates {
				items[i] = snapshot.Item{Raw: cr.Name + "@" + cr.Version, Columns: []string{cr.Name, cr.Version}}
			}
			out["runtimes.rust.crates"] = snapshot.Section{Items: items}
		}
	}

	if swiftOut, _ := c.Env.Run(c.Context, "swift", "--version"); true {
		if v := ParseSwiftVersion(swiftOut); v != "" {
			out["runtimes.swift"] = snapshot.Section{Pairs: map[string]string{"version": v}}
		}
	}

	if zigOut, _ := c.Env.Run(c.Context, "zig", "version"); true {
		if v := ParseZigVersion(zigOut); v != "" {
			out["runtimes.zig"] = snapshot.Section{Pairs: map[string]string{"version": v}}
		}
	}

	if xcodeOut, _ := c.Env.Run(c.Context, "xcodebuild", "-version"); true {
		if x := ParseXcodeVersion(xcodeOut); x != nil {
			pairs := map[string]string{"version": x.Version}
			if x.Build != "" {
				pairs["build"] = x.Build
			}
			if pathOut, _ := c.Env.Run(c.Context, "xcode-select", "-p"); strings.TrimSpace(pathOut) != "" {
				pairs["path"] = strings.TrimSpace(pathOut)
			}
			out["runtimes.xcode"] = snapshot.Section{Pairs: pairs}
		}
	}

	sdk := c.Env.Getenv("ANDROID_HOME")
	if sdk == "" {
		sdk = c.Env.Getenv("ANDROID_SDK_ROOT")
	}
	if sdk == "" {
		sdk = c.Home + "/Library/Android/sdk"
	}
	if c.Env.Exists(sdk) {
		pairs := map[string]string{"sdk": sdk}
		if adbOut, _ := c.Env.Run(c.Context, "adb", "version"); true {
			if v := ParseAdbVersion(adbOut); v != "" {
				pairs["platformTools"] = v
			}
		}
		out["runtimes.android"] = snapshot.Section{Pairs: pairs}

		if buildTools, err := c.Env.ListDir(sdk + "/build-tools"); err == nil && len(buildTools) > 0 {
			sort.Strings(buildTools)
			out["runtimes.android.buildTools"] = snapshot.Section{Items: toItems(buildTools)}
		}

		if platforms, err := c.Env.ListDir(sdk + "/platforms"); err == nil && len(platforms) > 0 {
			sort.Strings(platforms)
			out["runtimes.android.platforms"] = snapshot.Section{Items: toItems(platforms)}
		}
	}

	return out
}
