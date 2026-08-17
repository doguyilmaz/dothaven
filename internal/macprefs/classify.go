package macprefs

import (
	"regexp"
	"strings"
)

// Action is what should happen to a preference on a new machine.
type Action int

const (
	// Skip is the zero value on purpose: when nothing is known about a key,
	// not writing it is the answer that cannot break a Mac.
	Skip Action = iota
	// Review is a real setting whose value only makes sense on the machine it
	// came from — a path, an identifier. Captured and shown, never written.
	Review
	// Apply is a portable setting: replay it with `defaults write`.
	Apply
)

func (a Action) String() string {
	switch a {
	case Apply:
		return "apply"
	case Review:
		return "review"
	}
	return "skip"
}

// noise matches keys that are an app talking to itself: schema and migration
// markers, analytics stamps, launch counters, window and view geometry, recent
// item lists. They sit in the same domains as the real settings, so the only
// way to tell them apart is by name.
//
// Anchored and case-insensitive. RE2, so no backtracking blowup on a long key.
// Case is written out rather than using (?i): a case-insensitive `last[a-z]`
// also matches the middle of "plastic", and these patterns run against every
// key on the machine.
var noise = regexp.MustCompile(strings.Join([]string{
	`^NS(Window|Toolbar|SplitView|TableView|OutlineView|Nav|StatusItem|Drawer)`,
	`^AK[A-Z]`, // Apple account bookkeeping: AKLastLocale, AKLastIDMSEnvironment
	`^_`,
	`[Ww]indow(Location|Frame|Position|Size)`,
	`[Ll]ast[A-Z]`,
	`[-_]?[Cc]ount$`,
	`[Vv]ersion$`,
	`(Transition|Migration)Complete`,
	`[Dd]id[Mm]igrate|[Mm]igrated`,
	`[Aa]nalytics|[Tt]elemetry|[Dd]iagnostics?`,
	`[Rr]ecent`,
	`[Bb]ookmark|[Aa]lias[Dd]ata`,
	`[Tt]imestamp`,
}, "|"))

// identifier matches a value that only names a thing on the old machine — a
// boot, display or hardware UUID. Unlike a path, there is no version of it
// worth setting by hand here, so it is dropped rather than reported.
var identifier = regexp.MustCompile(
	`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)

// hostPath matches a value that points into the old machine's filesystem. The
// setting is real — where screenshots land, which folder a dialog opens — and
// the equivalent here is something a person can choose, so it is reported.
var hostPath = regexp.MustCompile(`^(/Users/|/Volumes/|/private/)`)

// Classify decides what to do with one preference, and says why.
//
// The order matters. Anything that is not a single value goes first, because
// that is where Spaces layouts and display UUIDs live and `defaults write`
// could not replay them regardless. Then bookkeeping by key name. Only then is
// the value examined, so a genuine setting that happens to hold a path is
// reported rather than dropped.
func Classify(domain, key string, v Value) (Action, string) {
	if v.Kind == Composite {
		return Skip, "not a single value"
	}
	if key == "" {
		return Skip, "empty key"
	}
	if noise.MatchString(key) {
		return Skip, "app bookkeeping, not a setting"
	}
	if identifier.MatchString(v.S) {
		return Skip, "an identifier, not a setting"
	}
	if hostPath.MatchString(v.S) {
		return Review, "path on the old machine"
	}
	return Apply, ""
}
