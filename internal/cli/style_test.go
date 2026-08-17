package cli

import (
	"strings"
	"testing"
)

// A long path used to push the detail column out of line, which is what made a
// list of twenty repositories unreadable.
func TestPadToHoldsTheColumn(t *testing.T) {
	long := "~/Developer/EaaS.Frontend.CSMS/EaaS.Mobile.Trugo.Availability"
	short := "~/Developer/FORK/panes"
	for _, p := range []string{long, short} {
		if got := len([]rune(padTo(p, pathCol))); got != pathCol {
			t.Errorf("padTo(%q) is %d columns, want %d", p, got, pathCol)
		}
	}
}

// The last segments name the repository. A character-level cut through
// "EaaS.Frontend.CSMS/EaaS.Mobile.Trugo.Availability" leaves
// "EaaS.Fron…ile.Trugo.Availability", which identifies nothing.
func TestShortenPathKeepsTheName(t *testing.T) {
	got := shortenPath("~/Developer/EaaS.Frontend.CSMS/EaaS.Mobile.Trugo.Availability", 44)
	if !strings.HasSuffix(got, "EaaS.Mobile.Trugo.Availability") {
		t.Errorf("the repository name must survive, got %q", got)
	}
	if !strings.HasPrefix(got, "…/") {
		t.Errorf("dropped segments should be marked, got %q", got)
	}
	if len([]rune(got)) > 44 {
		t.Errorf("still too long: %q", got)
	}
}

func TestShortenPathLeavesShortPathsAlone(t *testing.T) {
	p := "~/Developer/FORK/panes"
	if got := shortenPath(p, 44); got != p {
		t.Errorf("shortenPath(%q) = %q, want it untouched", p, got)
	}
}

// One segment longer than the whole column has no segment boundary to cut on.
func TestShortenPathFallsBackToACharacterCut(t *testing.T) {
	got := shortenPath("~/"+strings.Repeat("x", 80), 20)
	if len([]rune(got)) > 20 {
		t.Errorf("got %d runes, want at most 20: %q", len([]rune(got)), got)
	}
	if !strings.Contains(got, "…") {
		t.Errorf("a truncation should be visible, got %q", got)
	}
}

// Piped output has to stay parseable, so the helpers must be inert off a
// terminal — the tests themselves are the proof, since `go test` is not a TTY.
func TestStylesAreInertWhenNotATerminal(t *testing.T) {
	for name, got := range map[string]string{
		"bold": bold("x"), "dim": dim("x"), "good": good("x"),
		"warn": warn("x"), "danger": danger("x"), "kbd": kbd("x"),
	} {
		if got != "x" {
			t.Errorf("%s emitted %q off a terminal; piped output must stay plain", name, got)
		}
	}
}

func TestHeaderNamesTheAction(t *testing.T) {
	got := header("What's changed?")
	if !strings.Contains(got, "What's changed?") {
		t.Errorf("a separator that does not name the action solves half the problem: %q", got)
	}
}
