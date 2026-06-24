package cli

import "testing"

func TestMacDefaultsDomains(t *testing.T) {
	ds := macDefaultsDomains()
	if len(ds) == 0 {
		t.Fatal("expected a curated domain set")
	}
	seen := map[string]bool{}
	for _, d := range ds {
		if d.ID == "" || d.Name == "" {
			t.Errorf("domain missing ID/Name: %+v", d)
		}
		if seen[d.ID] {
			t.Errorf("duplicate domain %s", d.ID)
		}
		seen[d.ID] = true
		// System domains are intentionally excluded (host-specific keys).
		switch d.ID {
		case "com.apple.dock", "com.apple.finder", "NSGlobalDomain":
			t.Errorf("system domain %s must not be in the curated app-prefs set", d.ID)
		}
	}
	if !seen["com.googlecode.iterm2"] {
		t.Error("expected iTerm2 in the curated set")
	}
}

func TestDefaultsFileRoundTrip(t *testing.T) {
	if got := defaultsFileName("com.googlecode.iterm2"); got != "com.googlecode.iterm2.plist" {
		t.Errorf("defaultsFileName = %q", got)
	}
	if got := defaultsDomainFromFile("com.googlecode.iterm2.plist"); got != "com.googlecode.iterm2" {
		t.Errorf("defaultsDomainFromFile = %q", got)
	}
}

func TestDefaultsHasKeys(t *testing.T) {
	if !defaultsHasKeys(`<plist><dict><key>X</key><string>y</string></dict></plist>`) {
		t.Error("a plist with a key should report HasKeys")
	}
	if defaultsHasKeys(`<plist><dict/></plist>`) {
		t.Error("an empty dict should not report HasKeys")
	}
}
