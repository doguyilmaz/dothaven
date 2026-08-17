package macprefs

import "testing"

// Shaped exactly like `defaults export NSGlobalDomain -` output.
const sample = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>AppleInterfaceStyle</key>
	<string>Dark</string>
	<key>com.apple.swipescrolldirection</key>
	<false/>
	<key>AppleShowAllExtensions</key>
	<true/>
	<key>KeyRepeat</key>
	<integer>2</integer>
	<key>com.apple.trackpad.scaling</key>
	<real>1.5</real>
	<key>NSWindow Frame Main</key>
	<dict>
		<key>x</key>
		<integer>100</integer>
	</dict>
	<key>AppleLanguages</key>
	<array>
		<string>en-GB</string>
	</array>
	<key>NSNavRecentPlaces</key>
	<data>
	YnBsaXN0MDA=
	</data>
	<key>lastCheck</key>
	<date>2026-08-17T10:00:00Z</date>
</dict>
</plist>`

func TestParse(t *testing.T) {
	got, err := Parse([]byte(sample))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	want := map[string]Value{
		"AppleInterfaceStyle":            {String, "Dark"},
		"com.apple.swipescrolldirection": {Bool, "false"},
		"AppleShowAllExtensions":         {Bool, "true"},
		"KeyRepeat":                      {Int, "2"},
		"com.apple.trackpad.scaling":     {Float, "1.5"},
		"NSWindow Frame Main":            {Composite, ""},
		"AppleLanguages":                 {Composite, ""},
		"NSNavRecentPlaces":              {Composite, ""},
		"lastCheck":                      {Composite, ""},
	}
	if len(got) != len(want) {
		t.Errorf("got %d keys, want %d: %v", len(got), len(want), got)
	}
	for k, w := range want {
		g, ok := got[k]
		if !ok {
			t.Errorf("missing key %q", k)
			continue
		}
		if g != w {
			t.Errorf("%q = %+v, want %+v", k, g, w)
		}
	}

	// The decisive one: a key inside a nested dict must not surface as a
	// top-level preference. `defaults write` would create a junk key.
	if _, leaked := got["x"]; leaked {
		t.Error("nested key \"x\" leaked into the top level")
	}
}

func TestParseEdgeCases(t *testing.T) {
	empty := `<?xml version="1.0"?><plist version="1.0"><dict/></plist>`
	got, err := Parse([]byte(empty))
	if err != nil {
		t.Fatalf("empty plist: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("empty plist = %v, want no keys", got)
	}

	// `defaults export` of a domain that does not exist.
	if _, err := Parse([]byte(`<plist version="1.0"><dict/></plist>`)); err != nil {
		t.Errorf("bare dict: %v", err)
	}

	for _, bad := range []string{"", "not xml at all", "<plist><dict><key>a</key>"} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Errorf("Parse(%q) = nil error, want an error", bad)
		}
	}
}
