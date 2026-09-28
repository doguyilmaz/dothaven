// Package tui holds dothaven's interactive terminal flows (charmbracelet/huh).
// Every flow is opt-in: callers invoke it only when Interactive() is true, so
// piped and flag-driven runs stay non-interactive and CI-safe.
package tui

import (
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

// menuHintStyle renders the muted one-line explanation beside each menu action.
var menuHintStyle = lipgloss.NewStyle().Faint(true)

// Columns used on each line of a list before the label: huh's left border
// and padding, the cursor, and for a multi-select the [x] box.
const (
	selectIndent = 4
	multiIndent  = 8
)

// columns lays out each label with its hint beside it, fitted to the
// terminal: labels padded to a common width, hints cut short with "…" instead
// of wrapped, and dropped when the window has no room for them. A line that
// wraps moves the whole list under the cursor. note, when set, is kept whole
// after the hint (it says something that must not be cut, like "credentials").
func columns(labels, hints, notes []string, indent int) []string {
	room := termWidth() - indent - 1
	lw := 0
	for _, l := range labels {
		lw = max(lw, ansi.StringWidth(l))
	}
	lw = min(lw, room*3/5)
	out := make([]string, len(labels))
	for i, l := range labels {
		l = ansi.Truncate(l, room, "…")
		hint, note := at(hints, i), at(notes, i)
		left := room - max(lw, ansi.StringWidth(l)) - 2
		if hint == "" && note == "" || left < 12 {
			out[i] = l
			continue
		}
		pad := strings.Repeat(" ", max(lw-ansi.StringWidth(l), 0))
		tail := ""
		if note != "" {
			tail = "  " + note
			left -= ansi.StringWidth(tail)
		}
		if left < 8 {
			hint = ""
		}
		out[i] = l + pad + "  " + menuHintStyle.Render(ansi.Truncate(hint, max(left, 0), "…")) + tail
	}
	return out
}

func at(xs []string, i int) string {
	if i < len(xs) {
		return xs[i]
	}
	return ""
}

func termWidth() int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		return w
	}
	return 80
}

// Interactive reports whether both stdin and stdout are terminals, i.e. a prompt
// makes sense. Piped/redirected I/O (CI, `| cat`, `< file`) returns false.
func Interactive() bool {
	return isTTY(os.Stdin) && isTTY(os.Stdout)
}

func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// Group is one selectable category: its name, a plain-words description of
// what is in it, and a short note (e.g. that it holds credentials).
type Group struct {
	Name  string
	About string
	Note  string
}

// SelectCategories presents a multi-select of category groups (all pre-selected)
// and returns the chosen names. An empty selection with no error means the user
// deselected everything.
func SelectCategories(title, description string, groups []Group) ([]string, error) {
	if len(groups) == 0 {
		return nil, nil
	}
	names, abouts, notes := make([]string, len(groups)), make([]string, len(groups)), make([]string, len(groups))
	for i, g := range groups {
		names[i], abouts[i], notes[i] = g.Name, g.About, g.Note
	}
	opts := make([]huh.Option[string], len(groups))
	for i, label := range columns(names, abouts, notes, multiIndent) {
		opts[i] = huh.NewOption(label, groups[i].Name).Selected(true)
	}
	selected := make([]string, 0, len(groups))
	h := listHeight(len(groups), title, description)
	field := huh.NewMultiSelect[string]().
		Title(title).
		Description(description).
		Options(opts...).
		Filterable(h > 0).
		Height(h).
		Value(&selected)
	if err := runList(h > 0, field); err != nil {
		return nil, err
	}
	return selected, nil
}

// PickItem is one line of a multi-select.
type PickItem struct {
	Label, Value, Hint string
	Selected           bool
}

// MultiPick presents a multi-select with the given pre-selection. Long lists
// can be filtered by typing "/".
func MultiPick(title, description string, items []PickItem) ([]string, error) {
	if len(items) == 0 {
		return nil, nil
	}
	labels, hints := make([]string, len(items)), make([]string, len(items))
	for i, it := range items {
		labels[i], hints[i] = it.Label, it.Hint
	}
	opts := make([]huh.Option[string], len(items))
	for i, label := range columns(labels, hints, nil, multiIndent) {
		opts[i] = huh.NewOption(label, items[i].Value).Selected(items[i].Selected)
	}
	var picked []string
	h := listHeight(len(items), title, description)
	field := huh.NewMultiSelect[string]().
		Title(title).
		Description(description).
		Options(opts...).
		Filterable(h > 0).
		Height(h).
		Value(&picked)
	if err := runList(h > 0, field); err != nil {
		return nil, err
	}
	return picked, nil
}

// PickSome presents a multi-select with nothing chosen and returns the picks.
func PickSome(title, description string, items []string) ([]string, error) {
	if len(items) == 0 {
		return nil, nil
	}
	opts := make([]huh.Option[string], len(items))
	for i, label := range columns(items, nil, nil, multiIndent) {
		opts[i] = huh.NewOption(label, items[i])
	}
	var picked []string
	h := listHeight(len(items), title, description)
	field := huh.NewMultiSelect[string]().
		Title(title).
		Description(description).
		Options(opts...).
		Filterable(h > 0).
		Height(h).
		Value(&picked)
	if err := runList(h > 0, field); err != nil {
		return nil, err
	}
	return picked, nil
}

// Choice is one answer to a guided question. Hint is the muted line beside it,
// which is where the difference between two similar-sounding answers goes.
type Choice struct{ Label, Value, Hint string }

// ErrAborted reports that the user pressed Esc or Ctrl-C. Callers treat it as
// "never mind", not as a failure.
var ErrAborted = huh.ErrUserAborted

// Ask presents one question and returns the chosen value.
func Ask(title, description string, choices []Choice) (string, error) {
	labels, hints := make([]string, len(choices)), make([]string, len(choices))
	for i, c := range choices {
		labels[i], hints[i] = c.Label, c.Hint
	}
	opts := make([]huh.Option[string], len(choices))
	for i, label := range columns(labels, hints, nil, selectIndent) {
		opts[i] = huh.NewOption(label, choices[i].Value)
	}
	// The bound value must not match any option, or huh skips rendering the
	// options before the matched one until a keypress (huh#679).
	var choice string
	h := listHeight(len(opts), title, description)
	sel := huh.NewSelect[string]().Title(title).Description(description).Options(opts...).
		Height(h).Value(&choice)
	if err := runList(h > 0, sel); err != nil {
		return "", err
	}
	return choice, nil
}

// Confirm asks a yes/no question, with No preselected.
func Confirm(prompt string) (bool, error) { return ConfirmDefault(prompt, false) }

// ConfirmDefault asks a yes/no question with def preselected.
func ConfirmDefault(prompt string, def bool) (bool, error) {
	v := def
	if err := run(huh.NewConfirm().Title(prompt).Value(&v)); err != nil {
		return false, err
	}
	return v, nil
}

// Input asks for a line of text, returning def if left blank.
func Input(prompt, def string) (string, error) {
	v := def
	if err := run(huh.NewInput().Title(prompt).Value(&v)); err != nil {
		return "", err
	}
	if v == "" {
		return def, nil
	}
	return v, nil
}
