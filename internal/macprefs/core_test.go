package macprefs

import "testing"

func TestIsCore(t *testing.T) {
	// The settings someone re-sets by hand on a new Mac.
	for _, d := range []string{
		"NSGlobalDomain",            // natural scroll, key repeat, dark mode
		"com.apple.dock",            // dock size, hot corners
		"com.apple.finder",          // show all files, view options
		"com.apple.symbolichotkeys", // keyboard shortcuts
		"com.apple.screencapture",   // screenshot format
		"com.apple.AppleMultitouchTrackpad",
	} {
		if !IsCore(d) {
			t.Errorf("IsCore(%q) = false, want true", d)
		}
	}

	// Application internals, and Apple domains that only hold dismissal state.
	// Measured on a real machine: these were the biggest contributors to the
	// apply list, and none of them is a setting anybody chose.
	for _, d := range []string{
		"com.microsoft.Outlook",
		"eu.exelban.Stats",
		"com.parallels.Parallels Desktop",
		"com.apple.universalaccessAuthWarning",
		"com.apple.dt.Xcode",
		"",
	} {
		if IsCore(d) {
			t.Errorf("IsCore(%q) = true, want false", d)
		}
	}

	// A prefix match would pull in every com.apple.* domain, which is the
	// 279-domain list this exists to avoid.
	if IsCore("com.apple.universalaccessAuthWarning") && IsCore("com.apple.universalaccess") {
		t.Error("domain matching is by prefix; it must be exact")
	}
}
