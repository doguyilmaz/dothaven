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
	return huh.NewOption(fmt.Sprintf("%-37s %s", label, menuHintStyle.Render(hint)), value)
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

// Group is one selectable category with its entry count and whether it holds
// secrets (encrypted on export).
type Group struct {
	Name      string
	Count     int
	Encrypted bool
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
		label := fmt.Sprintf("%-10s", g.Name)
		if g.Count > 0 {
			label = fmt.Sprintf("%-10s %2d", g.Name, g.Count)
		}
		if g.Encrypted {
			label += "  🔒 encrypted"
		}
		opts[i] = huh.NewOption(label, g.Name).Selected(true)
	}
	selected := make([]string, 0, len(groups))
	field := huh.NewMultiSelect[string]().
		Title(title).
		Description("space toggles · enter confirms").
		Options(opts...).
		Value(&selected)
	if err := huh.NewForm(huh.NewGroup(field)).Run(); err != nil {
		return nil, err
	}
	return selected, nil
}

// MainMenu shows the top-level action picker and returns the chosen command
// name ("quit" = quit).
func MainMenu() (string, error) {
	// The bound value must NOT match any option's value, or huh fails to render
	// the options before the matched one until a keypress (huh#679). An empty
	// default matches nothing, so the cursor starts at the top and all render.
	var choice string
	sel := huh.NewSelect[string]().
		Title("dothaven").
		Description("pick an action").
		Options(
			menuOption("Set up this machine (chezmoi apply)", "migrate", "chezmoi apply + reinstall packages"),
			menuOption("Back up configs", "backup", "create a new local config backup"),
			menuOption("Export to chezmoi (age-encrypted)", "chezmoi-export", "stage configs; secrets encrypted"),
			menuOption("Restore from the latest backup", "restore", "apply the latest backup to ~"),
			menuOption("Check setup (chezmoi + age)", "init", "verify chezmoi + age are ready"),
			menuOption("Status of the latest backup", "status", "diff the latest backup vs ~ (read-only)"),
			menuOption("Quit", "quit", ""),
		).
		Value(&choice)
	if err := huh.NewForm(huh.NewGroup(sel)).Run(); err != nil {
		// Esc / Ctrl-C at the menu means quit, not an error to surface.
		if errors.Is(err, huh.ErrUserAborted) {
			return "quit", nil
		}
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
