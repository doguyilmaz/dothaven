package collect

import (
	"os"
	"path/filepath"
	"testing"
)

// Verbatim `xcrun simctl list runtimes`, including the unavailable entry Xcode
// leaves behind after an upgrade.
const simctlRuntimes = `== Runtimes ==
iOS 18.6 (18.6 - 22G86) - com.apple.CoreSimulator.SimRuntime.iOS-18-6
iOS 26.5 (26.5 - 23F77) - com.apple.CoreSimulator.SimRuntime.iOS-26-5
tvOS 18.0 (18.0 - 22J356) - com.apple.CoreSimulator.SimRuntime.tvOS-18-0
iOS 15.0 (15.0 - 19A339) - com.apple.CoreSimulator.SimRuntime.iOS-15-0 (unavailable, runtime profile not found)
`

func TestParseSimctlRuntimes(t *testing.T) {
	got := ParseSimctlRuntimes(simctlRuntimes)
	want := []string{"iOS 18.6 (18.6 - 22G86)", "iOS 26.5 (26.5 - 23F77)", "tvOS 18.0 (18.0 - 22J356)"}
	if len(got) != len(want) {
		t.Fatalf("got %d runtimes, want %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("runtime %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// An unavailable runtime is one Xcode can no longer load. Recording it would
// send you hunting on the new machine for something that cannot be installed
// there either.
func TestParseSimctlRuntimesDropsUnavailable(t *testing.T) {
	for _, r := range ParseSimctlRuntimes(simctlRuntimes) {
		if r == "iOS 15.0 (15.0 - 19A339)" {
			t.Error("an unavailable runtime was recorded as if it were installed")
		}
	}
}

func TestParseSimctlRuntimesIgnoresOtherSections(t *testing.T) {
	got := ParseSimctlRuntimes(`== Devices ==
iPhone 16 (ABC-123) (Shutdown)
== Runtimes ==
iOS 18.6 (18.6 - 22G86) - com.apple.CoreSimulator.SimRuntime.iOS-18-6
`)
	if len(got) != 1 || got[0] != "iOS 18.6 (18.6 - 22G86)" {
		t.Errorf("device rows must not be read as runtimes, got %q", got)
	}
}

func TestAndroidSDKPackagesReadsTheLayout(t *testing.T) {
	sdk := t.TempDir()
	for _, d := range []string{"platforms/android-35", "build-tools/34.0.0", "ndk/27.1.1", "licenses"} {
		os.MkdirAll(filepath.Join(sdk, d), 0o755)
	}
	got := AndroidSDKPackages(sdk)
	want := map[string]bool{"platforms/android-35": true, "build-tools/34.0.0": true, "ndk/27.1.1": true}
	if len(got) != len(want) {
		t.Fatalf("got %q, want the three versioned packages", got)
	}
	for _, g := range got {
		if !want[g] {
			t.Errorf("unexpected package %q — licenses is not one", g)
		}
	}
}

func TestAndroidSDKPackagesHandlesNoSDK(t *testing.T) {
	if got := AndroidSDKPackages(filepath.Join(t.TempDir(), "nope")); got != nil {
		t.Errorf("a machine with no Android SDK should yield nothing, got %q", got)
	}
	if got := AndroidSDKPackages(""); got != nil {
		t.Errorf("empty root should yield nothing, got %q", got)
	}
}

// The .ini is the definition; the .avd folder beside it is the disk image,
// which is large and rebuilt by the emulator.
func TestAndroidAVDsListsDefinitionsNotImages(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Pixel_10.ini"), []byte("path=..."), 0o644)
	os.WriteFile(filepath.Join(dir, "API30_gesture.ini"), []byte("path=..."), 0o644)
	os.MkdirAll(filepath.Join(dir, "Pixel_10.avd"), 0o755)

	got := AndroidAVDs(dir)
	if len(got) != 2 || got[0] != "API30_gesture" || got[1] != "Pixel_10" {
		t.Errorf("got %q, want the two definitions sorted", got)
	}
}

func TestAndroidSDKRootPrefersTheEnvironment(t *testing.T) {
	env := func(k string) string {
		if k == "ANDROID_HOME" {
			return "/custom/sdk"
		}
		return ""
	}
	if got := androidSDKRoot(env, "/Users/x"); got != "/custom/sdk" {
		t.Errorf("ANDROID_HOME should win, got %q", got)
	}
	if got := androidSDKRoot(func(string) string { return "" }, "/Users/x"); got != "/Users/x/Library/Android/sdk" {
		t.Errorf("default should be the macOS location, got %q", got)
	}
}
