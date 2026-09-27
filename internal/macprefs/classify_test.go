package macprefs

import "testing"

// Every key below was read off a real machine with `defaults export`, not
// invented — the noise in a preference domain is specific enough that guessing
// at it produces a classifier that works on nothing.
func TestClassify(t *testing.T) {
	tests := []struct {
		name   string
		domain string
		key    string
		value  Value
		want   Action
	}{
		// Settings somebody chose. These are the point of the whole feature.
		{"natural scroll", "NSGlobalDomain", "com.apple.swipescrolldirection", Value{Kind: Bool, S: "false"}, Apply},
		{"dark mode", "NSGlobalDomain", "AppleInterfaceStyle", Value{Kind: String, S: "Dark"}, Apply},
		{"key repeat", "NSGlobalDomain", "KeyRepeat", Value{Kind: Int, S: "2"}, Apply},
		{"full keyboard access", "NSGlobalDomain", "AppleKeyboardUIMode", Value{Kind: Int, S: "3"}, Apply},
		{"locale", "NSGlobalDomain", "AppleLocale", Value{Kind: String, S: "en_TR"}, Apply},
		{"show all extensions", "com.apple.finder", "AppleShowAllExtensions", Value{Kind: Bool, S: "true"}, Apply},
		{"dock autohide", "com.apple.dock", "autohide", Value{Kind: Bool, S: "true"}, Apply},
		{"dock tile size", "com.apple.dock", "tilesize", Value{Kind: Float, S: "48"}, Apply},
		{"screenshot format", "com.apple.screencapture", "type", Value{Kind: String, S: "png"}, Apply},
		{"third-party app pref", "com.knollsoft.Rectangle", "gapSize", Value{Kind: Int, S: "8"}, Apply},

		// Apple's own bookkeeping, sitting in the same domain as the settings.
		{"schema version", "NSGlobalDomain", "AppleLanguagesSchemaVersion", Value{Kind: Int, S: "5400"}, Skip},
		{"migration marker", "NSGlobalDomain", "AppleLanguagesDidMigrate", Value{Kind: String, S: "13.0"}, Skip},
		{"analytics timestamp", "NSGlobalDomain", "ACDMonthlyAnalyticsLastPosted", Value{Kind: Float, S: "808190931.9"}, Skip},
		{"account bookkeeping", "NSGlobalDomain", "AKLastIDMSEnvironment", Value{Kind: Int, S: "0"}, Skip},
		{"account locale", "NSGlobalDomain", "AKLastLocale", Value{Kind: String, S: "en_TR"}, Skip},
		{"last check", "com.apple.whatever", "LastUpdateCheckDate", Value{Kind: String, S: "x"}, Skip},
		{"launch counter", "com.apple.whatever", "launchCount", Value{Kind: Int, S: "42"}, Skip},

		// Leaks caught by running the classifier over a real machine — every
		// one of these was landing in Apply.
		{"last-prefixed interval", "NSGlobalDomain", "NSLinguisticDataAssetsRequestLastInterval", Value{Kind: Int, S: "86400"}, Skip},
		{"transition marker", "NSGlobalDomain", "NSSpellCheckerContainerTransitionComplete", Value{Kind: Bool, S: "true"}, Skip},
		{"last indicator time", "com.apple.dock", "lastShowIndicatorTime", Value{Kind: Float, S: "769675539.1"}, Skip},
		{"hyphenated counter", "com.apple.dock", "mod-count", Value{Kind: Int, S: "7903"}, Skip},
		{"bare version", "com.apple.dock", "version", Value{Kind: Int, S: "1"}, Skip},
		{"non-NS window geometry", "com.apple.finder", "CopyProgressWindowLocation", Value{Kind: String, S: "{587, 207}"}, Skip},
		// Not listed: com.apple.dock trash-full. It is transient state, but
		// there is no pattern that catches it without catching real settings,
		// and writing it is harmless — the Dock recomputes it immediately.

		// …and these must survive the tightening. Hot corners, function-key
		// behaviour and Spaces ordering are exactly what people re-set by hand.
		{"hot corner", "com.apple.dock", "wvous-br-corner", Value{Kind: Int, S: "1"}, Apply},
		{"fn key state", "NSGlobalDomain", "com.apple.keyboard.fnState", Value{Kind: Bool, S: "true"}, Apply},
		{"mru spaces", "com.apple.dock", "mru-spaces", Value{Kind: Bool, S: "false"}, Apply},
		{"open dialog view", "NSGlobalDomain", "NavPanelFileListModeForOpenMode", Value{Kind: Int, S: "2"}, Apply},
		{"accent colour", "NSGlobalDomain", "AppleAquaColorVariant", Value{Kind: Int, S: "1"}, Apply},
		{"springing delay", "NSGlobalDomain", "com.apple.springing.delay", Value{Kind: Float, S: "0.5"}, Apply},

		// Window and view state.
		{"window frame", "com.apple.finder", "NSWindow Frame Main", Value{Kind: String, S: "0 0 100 100"}, Skip},
		{"toolbar config", "com.apple.finder", "NSToolbar Configuration Browser", Value{Kind: String, S: "x"}, Skip},
		{"recent places", "com.apple.finder", "NSNavRecentPlaces", Value{Kind: String, S: "x"}, Skip},
		{"split view", "com.apple.dt.Xcode", "NSSplitView Subview Frames", Value{Kind: String, S: "x"}, Skip},

		// Not a single value: dictionaries, arrays, data blobs, dates. This is
		// where Spaces and display UUIDs live, and `defaults write` cannot
		// replay them from a flat value anyway.
		{"composite", "com.apple.spaces", "SpacesDisplayConfiguration", Value{Kind: Composite}, Skip},

		// Real settings whose value belongs to the old machine.
		{"screenshot location", "com.apple.screencapture", "location", Value{Kind: String, S: "/Users/someone/Desktop"}, Review},
		{"volume path", "com.apple.whatever", "libraryPath", Value{Kind: String, S: "/Volumes/Data/x"}, Review},
		// A UUID is never something to go and set by hand, so it is dropped
		// rather than put on a list of things to do. Listing boot UUIDs as
		// settings to restore is what this replaced.
		{"uuid value", "com.apple.whatever", "deviceIdentifier", Value{Kind: String, S: "5F2A9C41-7B3D-4E8A-9C1F-2D6B8E4A7C39"}, Skip},
		{"boot uuid", "com.apple.CrashReporter", "exceptionProcesses.bootUUID", Value{Kind: String, S: "9DEDB17B-ABED-4A11-B8C1-84BA21E5A3EC"}, Skip},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, why := Classify(tt.domain, tt.key, tt.value)
			if got != tt.want {
				t.Errorf("Classify(%q, %q, %+v) = %v (%s), want %v",
					tt.domain, tt.key, tt.value, got, why, tt.want)
			}
			if got != Apply && why == "" {
				t.Errorf("Classify(%q) gave %v with no reason; the report has nothing to show", tt.key, got)
			}
		})
	}
}

func TestClassifyNeverPanics(t *testing.T) {
	for _, k := range []string{"", " ", "\x00", "NSWindow", "a/b/c"} {
		Classify("", k, Value{Kind: String, S: ""})
		Classify("d", k, Value{Kind: Composite})
	}
}
