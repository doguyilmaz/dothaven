package collect

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/doguyilmaz/dothaven/internal/snapshot"
)

// MobileCollector records the mobile toolchain: simulator runtimes, Android SDK
// packages and AVD definitions.
//
// None of it is copied — a simulator runtime is gigabytes and an AVD is a disk
// image, both rebuildable from a name. What gets lost in a migration is the
// *list*: which iOS versions you tested against, which API levels you kept
// around, which device definitions you had set up. Rebuilding that from memory
// is the part that takes an afternoon, and nothing else here recorded it.
//
// Everything is read from disk or from one fast command; nothing shells out to
// sdkmanager, which starts a JVM to tell you what a directory listing already
// says.
func MobileCollector(c Ctx) snapshot.Snapshot {
	out := snapshot.Snapshot{}

	if runtimes := ParseSimctlRuntimes(runOrEmpty(c, "xcrun", "simctl", "list", "runtimes")); len(runtimes) > 0 {
		out["mobile.ios.runtimes"] = snapshot.Section{Items: rawItems(runtimes)}
	}

	sdk := androidSDKRoot(c.Env.Getenv, c.Home)
	if pkgs := AndroidSDKPackages(sdk); len(pkgs) > 0 {
		out["mobile.android.sdk"] = snapshot.Section{Items: rawItems(pkgs)}
	}
	if avds := AndroidAVDs(filepath.Join(c.Home, ".android", "avd")); len(avds) > 0 {
		out["mobile.android.avds"] = snapshot.Section{Items: rawItems(avds)}
	}
	return out
}

func runOrEmpty(c Ctx, args ...string) string {
	out, err := c.Env.Run(c.Context, args...)
	if err != nil {
		return ""
	}
	return out
}

func rawItems(values []string) []snapshot.Item {
	items := make([]snapshot.Item, 0, len(values))
	for _, v := range values {
		items = append(items, snapshot.Item{Raw: v})
	}
	return items
}

// simctlRuntimeRe matches a usable runtime line from `xcrun simctl list
// runtimes`: "iOS 18.6 (18.6 - 22G86) - com.apple.CoreSimulator...".
var simctlRuntimeRe = regexp.MustCompile(`^(\S[^(]*?)\s+\(([^)]*)\)\s+-\s+(\S+)$`)

// ParseSimctlRuntimes returns the installed simulator runtimes, newest listing
// order preserved. Unavailable runtimes are dropped: simctl keeps listing a
// runtime whose Xcode has gone, and recording one would send you looking for
// something the new machine cannot install either.
func ParseSimctlRuntimes(text string) []string {
	var out []string
	inRuntimes := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "==") {
			inRuntimes = strings.Contains(trimmed, "Runtimes")
			continue
		}
		if !inRuntimes || trimmed == "" {
			continue
		}
		if strings.Contains(trimmed, "unavailable") {
			continue
		}
		m := simctlRuntimeRe.FindStringSubmatch(trimmed)
		if m == nil {
			continue
		}
		out = append(out, strings.TrimSpace(m[1])+" ("+strings.TrimSpace(m[2])+")")
	}
	return out
}

// androidSDKRoot resolves the SDK location the way the tools do.
func androidSDKRoot(getenv func(string) string, home string) string {
	for _, key := range []string{"ANDROID_HOME", "ANDROID_SDK_ROOT"} {
		if v := getenv(key); v != "" {
			return v
		}
	}
	return filepath.Join(home, "Library", "Android", "sdk")
}

// androidSDKDirs are the SDK subtrees whose contents are versioned packages.
var androidSDKDirs = []string{"platforms", "build-tools", "ndk", "cmake", "system-images"}

// AndroidSDKPackages lists installed SDK packages by reading the SDK layout.
func AndroidSDKPackages(sdkRoot string) []string {
	if sdkRoot == "" {
		return nil
	}
	var out []string
	for _, dir := range androidSDKDirs {
		entries, err := os.ReadDir(filepath.Join(sdkRoot, dir))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				out = append(out, dir+"/"+e.Name())
			}
		}
	}
	sort.Strings(out)
	return out
}

// AndroidAVDs lists the emulator definitions in an avd directory. The .ini next
// to each .avd folder is the definition; the folder is the disk image, which is
// large and rebuilt by the emulator.
func AndroidAVDs(avdDir string) []string {
	entries, err := os.ReadDir(avdDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".ini") {
			out = append(out, strings.TrimSuffix(e.Name(), ".ini"))
		}
	}
	sort.Strings(out)
	return out
}
