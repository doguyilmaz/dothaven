package macprefs

// coreDomains are the domains that hold what people mean by "my Mac settings":
// the things you notice are wrong within a minute of logging into a new machine
// — scrolling direction, key repeat, Dock size, hot corners, Finder options,
// keyboard shortcuts.
//
// The wide capture reads every domain on the machine, several hundred of them,
// and most of what it finds is an application's own internal state. Replaying
// all of it would mean writing thousands of keys nobody chose. So the export
// stays wide, because reading costs nothing, and this list decides what gets
// written back unless somebody asks for everything.
var coreDomains = map[string]bool{
	"NSGlobalDomain": true, // scrolling, key repeat, appearance, text substitution

	"com.apple.finder":          true,
	"com.apple.dock":            true, // also hot corners (wvous-*) and Spaces
	"com.apple.spaces":          true,
	"com.apple.WindowManager":   true, // Stage Manager, click-wallpaper-to-reveal
	"com.apple.controlcenter":   true, // what shows in the menu bar
	"com.apple.menuextra.clock": true,
	"com.apple.screencapture":   true, // format, shadow, where screenshots land
	"com.apple.desktopservices": true, // .DS_Store on network and USB volumes

	"com.apple.symbolichotkeys": true, // keyboard shortcuts
	"com.apple.HIToolbox":       true, // input sources and the keyboard menu
	"com.apple.universalaccess": true, // zoom, pointer size, reduce motion

	"com.apple.AppleMultitouchTrackpad":                  true,
	"com.apple.driver.AppleBluetoothMultitouch.trackpad": true,
	"com.apple.AppleMultitouchMouse":                     true,
	"com.apple.driver.AppleBluetoothMultitouch.mouse":    true,
	"com.apple.driver.AppleHIDMouse":                     true,

	"com.apple.SoftwareUpdate": true, // automatic update policy
	"com.apple.CrashReporter":  true, // the crash dialog developers turn off
}

// IsCore reports whether a domain is applied by default.
func IsCore(domain string) bool { return coreDomains[domain] }

// CoreDomainCount is how many domains the default apply covers, for help text.
func CoreDomainCount() int { return len(coreDomains) }
