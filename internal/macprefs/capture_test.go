package macprefs

import (
	"sort"
	"strings"
	"testing"
)

func TestCollect(t *testing.T) {
	entries, counts, err := Collect("NSGlobalDomain", []byte(sample))
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	got := map[string]Entry{}
	for _, e := range entries {
		got[e.Key] = e
	}

	// Settings survive, with the type `defaults write` will need.
	if e := got["com.apple.swipescrolldirection"]; e.Action != "apply" || e.Type != "bool" || e.Value != "false" {
		t.Errorf("swipescrolldirection = %+v, want apply/bool/false", e)
	}
	if e := got["AppleInterfaceStyle"]; e.Action != "apply" || e.Type != "string" || e.Value != "Dark" {
		t.Errorf("AppleInterfaceStyle = %+v, want apply/string/Dark", e)
	}
	if e := got["KeyRepeat"]; e.Type != "int" {
		t.Errorf("KeyRepeat type = %q, want int", e.Type)
	}
	if e := got["com.apple.trackpad.scaling"]; e.Type != "float" {
		t.Errorf("trackpad.scaling type = %q, want float", e.Type)
	}

	// Composites and state never become entries — keeping them would make the
	// file mostly junk and the summary meaningless.
	for _, k := range []string{"NSWindow Frame Main", "AppleLanguages", "NSNavRecentPlaces", "lastCheck"} {
		if _, ok := got[k]; ok {
			t.Errorf("%q was kept, want it dropped", k)
		}
	}
	if counts.Skipped != 4 {
		t.Errorf("Skipped = %d, want 4", counts.Skipped)
	}
	if counts.Apply != 5 {
		t.Errorf("Apply = %d, want 5", counts.Apply)
	}

	// Deterministic order, like every other snapshot this tool writes.
	keys := make([]string, len(entries))
	for i, e := range entries {
		keys[i] = e.Key
	}
	if !sort.StringsAreSorted(keys) {
		t.Errorf("entries are not sorted by key: %v", keys)
	}
}

func TestCollectDoesNotLeakSecrets(t *testing.T) {
	// Preference domains do hold tokens — an app that stores an API key in its
	// prefs is common. Anything captured here is written to a file and can end
	// up in a backup, so it goes through the same scanner as everything else.
	const withToken = `<?xml version="1.0"?>
<plist version="1.0">
<dict>
	<key>apiToken</key>
	<string>ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789</string>
	<key>theme</key>
	<string>dark</string>
</dict>
</plist>`

	entries, counts, err := Collect("com.example.app", []byte(withToken))
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Value, "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789") {
			t.Fatalf("token survived into %+v", e)
		}
	}
	if counts.Secret == 0 {
		t.Error("Secret count = 0; the token was not noticed at all")
	}
	// The harmless setting in the same domain still comes through.
	var sawTheme bool
	for _, e := range entries {
		if e.Key == "theme" && e.Value == "dark" {
			sawTheme = true
		}
	}
	if !sawTheme {
		t.Error("a secret in one key dropped an unrelated key in the same domain")
	}
}

func TestWriteArgs(t *testing.T) {
	tests := []struct {
		entry Entry
		want  string
	}{
		{Entry{Domain: "NSGlobalDomain", Key: "com.apple.swipescrolldirection", Type: "bool", Value: "false"},
			"defaults write NSGlobalDomain com.apple.swipescrolldirection -bool false"},
		{Entry{Domain: "com.apple.dock", Key: "tilesize", Type: "int", Value: "50"},
			"defaults write com.apple.dock tilesize -int 50"},
		{Entry{Domain: "NSGlobalDomain", Key: "com.apple.springing.delay", Type: "float", Value: "0.5"},
			"defaults write NSGlobalDomain com.apple.springing.delay -float 0.5"},
		{Entry{Domain: "NSGlobalDomain", Key: "AppleInterfaceStyle", Type: "string", Value: "Dark"},
			"defaults write NSGlobalDomain AppleInterfaceStyle -string Dark"},
	}
	for _, tt := range tests {
		if got := strings.Join(WriteArgs(tt.entry), " "); got != tt.want {
			t.Errorf("WriteArgs = %q, want %q", got, tt.want)
		}
	}

	// An entry that is not safe to write must produce no command at all, so a
	// caller cannot accidentally run one.
	if got := WriteArgs(Entry{Domain: "d", Key: "k", Type: "string", Value: "/Users/x", Action: "review"}); got != nil {
		t.Errorf("WriteArgs on a review entry = %v, want nil", got)
	}
	if got := WriteArgs(Entry{Domain: "d", Key: "k", Type: "data", Value: "x"}); got != nil {
		t.Errorf("WriteArgs on an unknown type = %v, want nil", got)
	}
}
