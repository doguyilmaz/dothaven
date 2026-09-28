package cli

import "testing"

func TestStaleBrewRefs(t *testing.T) {
	files := map[string]string{
		".zprofile": `eval "$(/usr/local/bin/brew shellenv)"`,
		".zshrc":    `eval "$(/opt/homebrew/bin/brew shellenv)"`,
		".bashrc":   "alias ll=ls",
	}
	arm := func(p string) bool { return p == "/opt/homebrew/bin/brew" }
	got := staleBrewRefs(files, arm)
	if len(got) != 1 || got[0] != (brewRef{File: "~/.zprofile", Wrong: "/usr/local", Right: "/opt/homebrew"}) {
		t.Errorf("Apple Silicon: %+v", got)
	}
	// Both prefixes present (Rosetta Homebrew beside the native one): nothing wrong.
	both := func(p string) bool { return p == "/opt/homebrew/bin/brew" || p == "/usr/local/bin/brew" }
	if got := staleBrewRefs(files, both); len(got) != 0 {
		t.Errorf("both installed: %+v", got)
	}
	// No Homebrew here at all: that is reinstall's job, not a wrong path.
	if got := staleBrewRefs(files, func(string) bool { return false }); len(got) != 0 {
		t.Errorf("no brew: %+v", got)
	}
}
