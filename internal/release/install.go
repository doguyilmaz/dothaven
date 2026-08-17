package release

import (
	"path/filepath"
	"strings"
)

const (
	// ModulePath is the `go install` target.
	ModulePath = "github.com/doguyilmaz/dothaven/cmd/dothaven"
	// ReleasesURL is where a binary nobody's package manager owns comes from.
	ReleasesURL = "https://github.com/doguyilmaz/dothaven/releases/latest"

	caskName = "dothaven"
)

// Method is how the running dothaven binary got onto this machine.
type Method int

const (
	// Manual is the zero value on purpose: when we cannot tell, the safe
	// advice is "here is the release page", never "let me overwrite this".
	Manual Method = iota
	Homebrew
	GoInstall
)

// Detect classifies an install from the resolved path of the running binary.
//
// The point of knowing is to delegate. Homebrew records which version it put in
// the Caskroom; a binary that rewrites itself in place leaves that record
// describing a file that no longer exists, and the next `brew upgrade` silently
// throws the replacement away. So dothaven never writes over itself — it works
// out who owns the file and hands the job back to them.
func Detect(execPath, brewPrefix, goBin string) Method {
	if execPath == "" {
		return Manual
	}
	// Caskroom/Cellar identify a brew install even when `brew --prefix` could
	// not be run, which is the case on a machine where brew is not on PATH.
	if under(execPath, brewPrefix) ||
		strings.Contains(execPath, "/Caskroom/") ||
		strings.Contains(execPath, "/Cellar/") {
		return Homebrew
	}
	if goBin != "" && filepath.Dir(execPath) == strings.TrimSuffix(goBin, "/") {
		return GoInstall
	}
	return Manual
}

// under reports whether path sits inside dir, on a path-separator boundary.
// A plain prefix test would read /opt/homebrew-mine as part of /opt/homebrew
// and then run brew against a binary brew has never heard of.
func under(path, dir string) bool {
	if dir == "" {
		return false
	}
	dir = strings.TrimSuffix(dir, "/")
	return path == dir || strings.HasPrefix(path, dir+"/")
}

// Steps is the argv sequence that upgrades this install, nil when there is none.
func Steps(m Method) [][]string {
	switch m {
	case Homebrew:
		// `brew update` first, always. The tap is a git clone that only
		// refreshes on update, so upgrading against a stale clone reports
		// "already installed" for a version that has been published for hours.
		return [][]string{
			{"brew", "update"},
			{"brew", "upgrade", "--cask", caskName},
		}
	case GoInstall:
		return [][]string{{"go", "install", ModulePath + "@latest"}}
	}
	return nil
}

// Command renders Steps as the shell line to show the reader, so what is
// printed and what would run cannot drift apart.
func Command(m Method) string {
	steps := Steps(m)
	if len(steps) == 0 {
		return ""
	}
	parts := make([]string, 0, len(steps))
	for _, s := range steps {
		parts = append(parts, strings.Join(s, " "))
	}
	return strings.Join(parts, " && ")
}
