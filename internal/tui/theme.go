package tui

import (
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// theme is the look of every prompt, matching the menu: the terminal's own
// palette, green for what is selected, and the terminal's own text colour for
// everything else, so it reads on a light background as well as a dark one.
func theme() *huh.Theme {
	t := huh.ThemeBase()
	green := lipgloss.Color("2")
	plain := lipgloss.NewStyle()
	faint := lipgloss.NewStyle().Faint(true)
	button := lipgloss.NewStyle().Padding(0, 2).MarginRight(1)

	f := &t.Focused
	f.Base = f.Base.BorderForeground(green)
	f.Card = f.Base
	f.Title = plain.Bold(true)
	f.NoteTitle = plain.Bold(true).MarginBottom(1)
	f.Description = faint
	f.ErrorIndicator = f.ErrorIndicator.Foreground(lipgloss.Color("1"))
	f.ErrorMessage = f.ErrorMessage.Foreground(lipgloss.Color("1"))
	f.SelectSelector = plain.Foreground(green).Bold(true).SetString("▸ ")
	f.MultiSelectSelector = plain.Foreground(green).Bold(true).SetString("▸ ")
	f.Option = plain
	f.UnselectedOption = plain
	f.SelectedOption = plain.Foreground(green).Bold(true)
	f.SelectedPrefix = plain.Foreground(green).SetString("[x] ")
	f.UnselectedPrefix = faint.SetString("[ ] ")
	f.NextIndicator = plain.Foreground(green).MarginLeft(1).SetString("→")
	f.PrevIndicator = plain.Foreground(green).MarginRight(1).SetString("←")
	f.FocusedButton = button.Bold(true).Foreground(lipgloss.Color("0")).Background(green)
	f.BlurredButton = button.Faint(true)
	f.TextInput.Cursor = plain.Foreground(green)
	f.TextInput.Placeholder = faint
	f.TextInput.Prompt = plain.Foreground(green)

	t.Blurred = t.Focused
	t.Blurred.Base = t.Blurred.Base.BorderStyle(lipgloss.HiddenBorder())
	t.Blurred.Card = t.Blurred.Base
	t.Blurred.Title = faint
	t.Blurred.NoteTitle = faint
	t.Blurred.SelectSelector = plain.SetString("  ")
	t.Blurred.MultiSelectSelector = plain.SetString("  ")
	t.Blurred.NextIndicator = plain
	t.Blurred.PrevIndicator = plain

	t.Group.Title = t.Focused.Title
	t.Group.Description = t.Focused.Description
	return t
}

// run shows one group of fields in dothaven's look.
func run(fields ...huh.Field) error {
	return huh.NewForm(huh.NewGroup(fields...)).WithTheme(theme()).Run()
}

// listHeight is the height to give a list prompt with n choices under its
// title and description (huh counts both), or 0 when the whole list fits the
// window. 0 matters: with any height set, huh scrolls the list as the cursor
// nears the bottom, even when every choice would fit, and the list appears
// to jump up under the cursor. Without one, it draws every choice and never
// scrolls. Only a list taller than the window gets a height, and scrolls.
func listHeight(n int, title, description string) int {
	rows := 24
	if _, h, err := term.GetSize(int(os.Stdout.Fd())); err == nil && h > 0 {
		rows = h
	}
	head := lines(title) + lines(description)
	if n+head <= rows-4 {
		return 0
	}
	return max(rows-4, head+3)
}

func lines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}
