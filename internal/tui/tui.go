// Package tui holds dothaven's interactive terminal flows (charmbracelet/huh).
// Every flow is opt-in: callers invoke it only when Interactive() is true, so
// piped and flag-driven runs stay non-interactive and CI-safe.
package tui

import (
	"errors"
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// menuHintStyle renders the muted one-line explanation beside each menu action.
var menuHintStyle = lipgloss.NewStyle().Faint(true)

// menuOption builds a menu entry whose label is followed by a muted hint,
// aligned in a column so the menu reads like "action — what it does".
func menuOption(label, value, hint string) huh.Option[string] {
	if hint == "" {
		return huh.NewOption(label, value)
	}
	return huh.NewOption(fmt.Sprintf("%-40s %s", label, menuHintStyle.Render(hint)), value)
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
func SelectCategories(title string, groups []Group) ([]string, error) {
	if len(groups) == 0 {
		return nil, nil
	}
	opts := make([]huh.Option[string], len(groups))
	for i, g := range groups {
		label := fmt.Sprintf("%-10s %s", g.Name, menuHintStyle.Render(g.About))
		if g.Note != "" {
			label += "  " + g.Note
		}
		opts[i] = huh.NewOption(label, g.Name).Selected(true)
	}
	selected := make([]string, 0, len(groups))
	field := huh.NewMultiSelect[string]().
		Title(title).
		Description("Everything is selected. space toggles · a toggles all · enter continues").
		Options(opts...).
		Height(min(len(groups)+4, 22)).
		Value(&selected)
	if err := huh.NewForm(huh.NewGroup(field)).Run(); err != nil {
		return nil, err
	}
	return selected, nil
}

// PickSome presents a multi-select with nothing chosen and returns the picks.
func PickSome(title, description string, items []string) ([]string, error) {
	if len(items) == 0 {
		return nil, nil
	}
	opts := make([]huh.Option[string], len(items))
	for i, it := range items {
		opts[i] = huh.NewOption(it, it)
	}
	var picked []string
	field := huh.NewMultiSelect[string]().
		Title(title).
		Description(description).
		Options(opts...).
		Height(min(len(items)+4, 20)).
		Value(&picked)
	if err := huh.NewForm(huh.NewGroup(field)).Run(); err != nil {
		return nil, err
	}
	return picked, nil
}

// MenuItem is one line of a menu. A Heading groups the lines under it and
// cannot be chosen.
type MenuItem struct {
	Label, Value, Hint string
	Heading            bool
}

var headingStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))

// headingValue marks a heading; choosing one just shows the menu again.
const headingValue = "\x00heading"

// Menu shows a list of actions under headings and returns the chosen value.
// Esc or Ctrl-C returns "quit".
func Menu(title, description string, items []MenuItem) (string, error) {
	opts := make([]huh.Option[string], 0, len(items))
	for i, it := range items {
		if it.Heading {
			opts = append(opts, huh.NewOption(headingStyle.Render(it.Label), fmt.Sprintf("%s%d", headingValue, i)))
			continue
		}
		opts = append(opts, menuOption("  "+it.Label, it.Value, it.Hint))
	}
	for {
		// The bound value must NOT match any option's value, or huh fails to
		// render the options before the matched one until a keypress (huh#679).
		var choice string
		sel := huh.NewSelect[string]().Title(title).Description(description).
			Options(opts...).Height(min(len(opts)+2, 30)).Value(&choice)
		if err := huh.NewForm(huh.NewGroup(sel)).Run(); err != nil {
			if errors.Is(err, huh.ErrUserAborted) {
				return "quit", nil
			}
			return "", err
		}
		if len(choice) >= len(headingValue) && choice[:len(headingValue)] == headingValue {
			continue
		}
		return choice, nil
	}
}

// Choice is one answer to a guided question. Hint is the muted line beside it,
// which is where the difference between two similar-sounding answers goes.
type Choice struct{ Label, Value, Hint string }

// ErrAborted reports that the user pressed Esc or Ctrl-C. Callers treat it as
// "never mind", not as a failure.
var ErrAborted = huh.ErrUserAborted

// Ask presents one question and returns the chosen value.
func Ask(title, description string, choices []Choice) (string, error) {
	opts := make([]huh.Option[string], 0, len(choices))
	for _, c := range choices {
		opts = append(opts, menuOption(c.Label, c.Value, c.Hint))
	}
	// The bound value must not match any option, or huh skips rendering the
	// options before the matched one until a keypress (huh#679).
	var choice string
	sel := huh.NewSelect[string]().Title(title).Description(description).Options(opts...).Value(&choice)
	if err := huh.NewForm(huh.NewGroup(sel)).Run(); err != nil {
		return "", err
	}
	return choice, nil
}

// Confirm asks a yes/no question.
func Confirm(prompt string) (bool, error) {
	var v bool
	if err := huh.NewForm(huh.NewGroup(huh.NewConfirm().Title(prompt).Value(&v))).Run(); err != nil {
		return false, err
	}
	return v, nil
}

// Input asks for a line of text, returning def if left blank.
func Input(prompt, def string) (string, error) {
	v := def
	if err := huh.NewForm(huh.NewGroup(huh.NewInput().Title(prompt).Value(&v))).Run(); err != nil {
		return "", err
	}
	if v == "" {
		return def, nil
	}
	return v, nil
}
