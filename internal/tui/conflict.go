package tui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// ConflictChoice is the user's decision for one restore conflict.
type ConflictChoice int

const (
	ChoiceSkip ConflictChoice = iota
	ChoiceOverwrite
	ChoiceOverwriteAll
	ChoiceSkipAll
)

var (
	diffDel = lipgloss.NewStyle().Foreground(lipgloss.Color("1")) // red
	diffAdd = lipgloss.NewStyle().Foreground(lipgloss.Color("2")) // green
)

const maxDiffLines = 40

// RenderDiff shows a line-by-line diff between the live file (old) and the
// backup (new): removed lines in red, added in green. Naive (line-indexed, not
// LCS) — enough to eyeball a conflict before overwriting.
func RenderDiff(oldText, newText string) string {
	oldLines := strings.Split(oldText, "\n")
	newLines := strings.Split(newText, "\n")
	n := len(oldLines)
	if len(newLines) > n {
		n = len(newLines)
	}
	var b strings.Builder
	shown := 0
	for i := 0; i < n && shown < maxDiffLines; i++ {
		o, nw := "", ""
		if i < len(oldLines) {
			o = oldLines[i]
		}
		if i < len(newLines) {
			nw = newLines[i]
		}
		if o == nw {
			continue
		}
		if o != "" {
			b.WriteString(diffDel.Render("- "+o) + "\n")
		}
		if nw != "" {
			b.WriteString(diffAdd.Render("+ "+nw) + "\n")
		}
		shown++
	}
	if shown == 0 {
		return "  (no textual differences)\n"
	}
	if shown >= maxDiffLines {
		b.WriteString(fmt.Sprintf("  … (truncated at %d changed lines)\n", maxDiffLines))
	}
	return b.String()
}

// ResolveConflict prompts for one file conflict, re-prompting after showing a
// diff, and returns the chosen action.
func ResolveConflict(path, backupContent, liveContent string) (ConflictChoice, error) {
	for {
		var choice string
		sel := huh.NewSelect[string]().
			Title("Conflict — "+path).
			Description("the live file differs from the backup").
			Options(
				huh.NewOption("Overwrite with backup", "o"),
				huh.NewOption("Skip (keep live file)", "s"),
				huh.NewOption("Show diff", "d"),
				huh.NewOption("Overwrite all remaining", "oa"),
				huh.NewOption("Skip all remaining", "sa"),
			).
			Value(&choice)
		if err := huh.NewForm(huh.NewGroup(sel)).Run(); err != nil {
			// Ctrl-C at a prompt aborts the whole restore (skip every remaining
			// conflict), not just this one file — otherwise the user has to
			// interrupt once per conflict.
			if errors.Is(err, huh.ErrUserAborted) {
				return ChoiceSkipAll, nil
			}
			return ChoiceSkip, err
		}
		switch choice {
		case "o":
			return ChoiceOverwrite, nil
		case "s":
			return ChoiceSkip, nil
		case "oa":
			return ChoiceOverwriteAll, nil
		case "sa":
			return ChoiceSkipAll, nil
		case "d":
			fmt.Println(RenderDiff(liveContent, backupContent))
		}
	}
}
