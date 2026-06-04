// Package tui holds dothaven's interactive terminal flows (charmbracelet/huh).
// Every flow is opt-in: callers invoke it only when Interactive() is true, so
// piped and flag-driven runs stay non-interactive and CI-safe.
package tui

import (
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
)

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
