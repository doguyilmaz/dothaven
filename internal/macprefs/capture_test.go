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

	// The language order is an array, and one of the few nested values that
	// is carried: kept whole as a compact fragment `defaults write` accepts.
	if e := got["AppleLanguages"]; e.Action != "apply" || e.Type != "plist" || e.Value != "<array><string>en-GB</string></array>" {
		t.Errorf("AppleLanguages = %+v, want apply/plist/<array>…", e)
	}

	// Other composites and state never become entries — keeping them would
	// make the file mostly junk and the summary meaningless.
	for _, k := range []string{"NSWindow Frame Main", "NSNavRecentPlaces", "lastCheck"} {
		if _, ok := got[k]; ok {
			t.Errorf("%q was kept, want it dropped", k)
		}
	}
	if counts.Skipped != 3 {
		t.Errorf("Skipped = %d, want 3", counts.Skipped)
	}
	if counts.Apply != 6 {
		t.Errorf("Apply = %d, want 6", counts.Apply)
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
	<key>syncApiToken</key>
	<string>c2a1f0e9d8b7a6c5d4e3f2a1b0c9d8e7</string>
</dict>
</plist>`

	entries, counts, err := Collect("com.example.app", []byte(withToken))
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	for _, e := range entries {
		// The prefixed token, and the opaque one only its key name gives away.
		if strings.Contains(e.Value, "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789") || strings.Contains(e.Value, "c2a1f0e9d8b7a6c5") {
			t.Fatalf("token survived into %+v", e)
		}
		if e.Key == "syncApiToken" && e.Action != "review" {
			t.Errorf("a redacted value must not be written back: %+v", e)
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
		// No type flag: the fragment is parsed as the array it spells.
		{Entry{Domain: "NSGlobalDomain", Key: "AppleLanguages", Type: "plist", Value: "<array><string>tr-TR</string></array>"},
			"defaults write NSGlobalDomain AppleLanguages <array><string>tr-TR</string></array>"},
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
	// A plist entry that is not an array or dict fragment would be stored as
	// a string — never written.
	if got := WriteArgs(Entry{Domain: "d", Key: "k", Type: "plist", Value: "(a, b)"}); got != nil {
		t.Errorf("WriteArgs on a non-fragment plist = %v, want nil", got)
	}
}

func TestCollectKeepsShortcutsAndInputSources(t *testing.T) {
	const hotkeys = `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
	<key>AppleSymbolicHotKeys</key>
	<dict>
		<key>64</key>
		<dict>
			<key>enabled</key>
			<false/>
			<key>value</key>
			<dict>
				<key>parameters</key>
				<array>
					<integer>32</integer>
					<integer>49</integer>
					<integer>1048576</integer>
				</array>
				<key>type</key>
				<string>standard</string>
			</dict>
		</dict>
	</dict>
	<key>SomethingElse</key>
	<dict>
		<key>a</key>
		<string>b</string>
	</dict>
</dict>
</plist>`
	entries, _, err := Collect("com.apple.symbolichotkeys", []byte(hotkeys))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Key != "AppleSymbolicHotKeys" {
		t.Fatalf("entries = %+v, want only AppleSymbolicHotKeys", entries)
	}
	want := "<dict><key>64</key><dict><key>enabled</key><false/><key>value</key><dict><key>parameters</key>" +
		"<array><integer>32</integer><integer>49</integer><integer>1048576</integer></array>" +
		"<key>type</key><string>standard</string></dict></dict></dict>"
	if entries[0].Value != want {
		t.Errorf("fragment =\n%s\nwant\n%s", entries[0].Value, want)
	}

	// Formatting does not matter: the same value exported compactly compares
	// equal, which is how "already set" is decided.
	compact := strings.NewReplacer("\n", "", "\t", "").Replace(hotkeys)
	again, _, _ := Collect("com.apple.symbolichotkeys", []byte(compact))
	if len(again) != 1 || again[0].Value != want {
		t.Errorf("compact export gave %+v", again)
	}

	// App Shortcuts are carried from any app's domain; text inside a value,
	// spaces and escapes included, survives exactly.
	const app = `<plist version="1.0"><dict><key>NSUserKeyEquivalents</key><dict>` +
		`<key>Save As &amp; Export…</key><string>@$s</string><key> Padded </key><string> x </string></dict></dict></plist>`
	entries, _, _ = Collect("com.apple.Safari", []byte(app))
	if len(entries) != 1 || entries[0].Type != "plist" ||
		entries[0].Value != "<dict><key>Save As &amp; Export…</key><string>@$s</string><key> Padded </key><string> x </string></dict>" {
		t.Errorf("app shortcuts = %+v", entries)
	}
	if !IsCoreEntry("com.apple.Safari", "NSUserKeyEquivalents") || IsCoreEntry("com.apple.Safari", "ShowFavoritesBar") {
		t.Error("App Shortcuts should apply by default in any domain, nothing else there should")
	}

	// A nested value nobody listed stays skipped, however it looks.
	const spaces = `<plist version="1.0"><dict><key>SpacesDisplayConfiguration</key><dict><key>x</key><string>y</string></dict></dict></plist>`
	if entries, _, _ := Collect("com.apple.spaces", []byte(spaces)); len(entries) != 0 {
		t.Errorf("spaces layout was kept: %+v", entries)
	}
	// Empty values self-close, so a round trip matches the export.
	const empty = `<plist version="1.0"><dict><key>AppleLanguages</key><array></array></dict></plist>`
	if entries, _, _ := Collect("NSGlobalDomain", []byte(empty)); len(entries) != 1 || entries[0].Value != "<array/>" {
		t.Errorf("empty array = %+v", entries)
	}
}

func TestFragmentTooLargeIsDropped(t *testing.T) {
	big := "<plist><dict><key>NSUserKeyEquivalents</key><dict>" +
		strings.Repeat("<key>k</key><string>"+strings.Repeat("x", 1000)+"</string>", 300) +
		"</dict><key>after</key><string>still read</string></dict></plist>"
	entries, _, err := Collect("com.example", []byte(big))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Key != "after" {
		t.Errorf("entries = %+v, want only the key after the oversized value", entries)
	}
}
