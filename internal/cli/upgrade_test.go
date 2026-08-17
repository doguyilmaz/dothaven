package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/doguyilmaz/dothaven/internal/sys"
)

func TestUpdateCheckAllowed(t *testing.T) {
	tests := []struct {
		name      string
		version   string
		optedOut  bool
		inCI      bool
		stderrTTY bool
		want      bool
	}{
		{"released binary on a terminal", "0.4.0", false, false, true, true},
		// A `go build` binary has no version to compare; nagging someone
		// working on dothaven itself is pure noise.
		{"dev build", "dev", false, false, true, false},
		{"opted out", "0.4.0", true, false, true, false},
		{"running in CI", "0.4.0", false, true, true, false},
		// The decisive one. dothaven's output is parsed — snapshots are
		// deterministic JSON — so a run whose stderr is not a terminal is a
		// run that must produce no banner at all.
		{"not a terminal", "0.4.0", false, false, false, false},
		{"opted out beats everything", "0.4.0", true, false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := updateCheckAllowed(tt.version, tt.optedOut, tt.inCI, tt.stderrTTY)
			if got != tt.want {
				t.Errorf("updateCheckAllowed(%q, %v, %v, %v) = %v, want %v",
					tt.version, tt.optedOut, tt.inCI, tt.stderrTTY, got, tt.want)
			}
		})
	}
}

func TestUpdateNotice(t *testing.T) {
	var buf bytes.Buffer
	updateNotice(&buf, "0.4.0", "0.5.0")
	got := buf.String()

	// Both versions, so the reader can see the size of the jump, and the exact
	// thing to type next — the whole point is that they do not have to go and
	// find out how this was installed.
	for _, want := range []string{"0.4.0", "0.5.0", "dothaven upgrade"} {
		if !strings.Contains(got, want) {
			t.Errorf("notice %q does not mention %q", got, want)
		}
	}
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("notice %q does not end in a newline", got)
	}
	// One line. A boxed multi-line banner after every command is the thing
	// people disable, and a disabled notice tells nobody anything.
	if n := strings.Count(strings.TrimSuffix(got, "\n"), "\n"); n != 0 {
		t.Errorf("notice spans %d extra lines, want a single line:\n%s", n, got)
	}
}

func TestUpdateCacheIsNotInTheDataDir(t *testing.T) {
	// Two things live in the data dir that a file written on every single
	// command must stay away from:
	//
	//   doctor, given no snapshot, falls back to the newest .json there — and
	//   an update cache rewritten on every run is always the newest .json, so
	//   doctor would try to parse it as a snapshot and fail.
	//
	//   backups are written owner-only (0700 dirs). MkdirAll does not tighten
	//   a directory that already exists, so a 0755 cache write landing first
	//   would leave the data dir world-listable for good.
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	env := sys.Real()
	cache := newUpdateChecker(env, "0.4.0").CachePath
	if strings.HasPrefix(cache, env.DataDir()) {
		t.Errorf("update cache %q is inside the data dir %q", cache, env.DataDir())
	}
	if filepath.Ext(cache) == ".json" && filepath.Dir(cache) == env.DataDir() {
		t.Errorf("update cache %q would be picked up by doctor's snapshot fallback", cache)
	}
}

func TestNoticeIsSuppressedForUpgradeItself(t *testing.T) {
	// Running `upgrade` and being told at the end that an upgrade is available
	// reads as a failure — especially straight after one succeeded, since the
	// running process still reports the version it was built as.
	root := NewRoot(sys.Real(), "0.4.0")

	tests := []struct {
		name string
		args []string
		want bool
	}{
		{"upgrade", []string{"upgrade"}, true},
		{"upgrade with flags", []string{"upgrade", "--check"}, true},
		{"the update alias", []string{"update"}, true},
		{"no arguments", nil, false},
		{"version flag", []string{"--version"}, false},
		{"an ordinary command", []string{"scan", "somefile"}, false},
		{"a command that does not exist", []string{"nonsense"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := suppressNotice(root, tt.args); got != tt.want {
				t.Errorf("suppressNotice(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestGoBinDir(t *testing.T) {
	env := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}
	tests := []struct {
		name string
		vars map[string]string
		home string
		want string
	}{
		{"GOBIN wins", map[string]string{"GOBIN": "/custom/bin"}, "/home/u", "/custom/bin"},
		{"GOPATH/bin", map[string]string{"GOPATH": "/gp"}, "/home/u", "/gp/bin"},
		// GOPATH is a list; `go install` uses the first entry.
		{"first GOPATH entry", map[string]string{"GOPATH": "/a:/b"}, "/home/u", "/a/bin"},
		{"default", nil, "/home/u", "/home/u/go/bin"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := goBinDir(env(tt.vars), tt.home); got != tt.want {
				t.Errorf("goBinDir = %q, want %q", got, tt.want)
			}
		})
	}
}
