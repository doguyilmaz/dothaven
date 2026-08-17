package release

import (
	"strings"
	"testing"
)

func TestDetect(t *testing.T) {
	const brew = "/opt/homebrew"
	const gobin = "/Users/u/go/bin"

	tests := []struct {
		name       string
		execPath   string
		brewPrefix string
		goBin      string
		want       Method
	}{
		// The real path on the machine this was written for: /opt/homebrew/bin
		// is a symlink into the versioned Caskroom directory, and os.Executable
		// is resolved before it gets here.
		{"cask artifact", "/opt/homebrew/Caskroom/dothaven/0.4.0/dothaven", brew, gobin, Homebrew},
		{"brew bin", "/opt/homebrew/bin/dothaven", brew, gobin, Homebrew},
		{"intel prefix", "/usr/local/Caskroom/dothaven/0.4.0/dothaven", "/usr/local", gobin, Homebrew},
		// Caskroom/Cellar identify brew even when `brew --prefix` failed.
		{"caskroom without prefix", "/opt/homebrew/Caskroom/dothaven/0.4.0/dothaven", "", "", Homebrew},
		{"cellar without prefix", "/opt/homebrew/Cellar/dothaven/0.4.0/bin/dothaven", "", "", Homebrew},

		{"go install", "/Users/u/go/bin/dothaven", brew, gobin, GoInstall},
		{"go bin not configured", "/Users/u/go/bin/dothaven", brew, "", Manual},

		{"loose binary", "/usr/local/bin/dothaven", brew, gobin, Manual},
		{"unresolvable", "", brew, gobin, Manual},
		// A prefix match without a separator boundary would claim this one for
		// Homebrew and then run brew against a binary brew has never seen.
		{"prefix is not a substring match", "/opt/homebrew-mine/bin/dothaven", brew, gobin, Manual},
		// Same trap on the Go side.
		{"go bin is not a substring match", "/Users/u/go/binaries/dothaven", brew, gobin, Manual},
		// A nested directory under go/bin is not something `go install` made.
		{"nested under go bin", "/Users/u/go/bin/old/dothaven", brew, gobin, Manual},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Detect(tt.execPath, tt.brewPrefix, tt.goBin); got != tt.want {
				t.Errorf("Detect(%q, %q, %q) = %v, want %v",
					tt.execPath, tt.brewPrefix, tt.goBin, got, tt.want)
			}
		})
	}
}

func TestSteps(t *testing.T) {
	// brew update first: the tap is a git clone, and an upgrade against a stale
	// clone reports "already installed" for a version that has been out for
	// hours. That is the exact failure this command exists to prevent.
	got := Steps(Homebrew)
	want := [][]string{
		{"brew", "update"},
		{"brew", "upgrade", "--cask", "dothaven"},
	}
	if len(got) != len(want) {
		t.Fatalf("Steps(Homebrew) = %v, want %v", got, want)
	}
	for i := range want {
		if strings.Join(got[i], " ") != strings.Join(want[i], " ") {
			t.Errorf("Steps(Homebrew)[%d] = %v, want %v", i, got[i], want[i])
		}
	}

	if got := Steps(GoInstall); len(got) != 1 || got[0][0] != "go" ||
		!strings.HasSuffix(got[0][len(got[0])-1], "@latest") {
		t.Errorf("Steps(GoInstall) = %v, want a single `go install ...@latest`", got)
	}

	// Nothing to run for a loose binary — the command must fall back to
	// printing the release page rather than executing something.
	if got := Steps(Manual); got != nil {
		t.Errorf("Steps(Manual) = %v, want nil", got)
	}
}

func TestCommandRendersSteps(t *testing.T) {
	// One source of truth: what we print is what we would run.
	if got, want := Command(Homebrew), "brew update && brew upgrade --cask dothaven"; got != want {
		t.Errorf("Command(Homebrew) = %q, want %q", got, want)
	}
	if got, want := Command(GoInstall), "go install github.com/doguyilmaz/dothaven/cmd/dothaven@latest"; got != want {
		t.Errorf("Command(GoInstall) = %q, want %q", got, want)
	}
	if got := Command(Manual); got != "" {
		t.Errorf("Command(Manual) = %q, want empty", got)
	}
}
