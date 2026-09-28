package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/doguyilmaz/dothaven/internal/tui"
)

// confirmWrite decides whether a command that changes this machine may run.
//
// One rule in one place: the commands that write used to answer this question
// five different ways, and `migrate`, which overwrites $HOME and runs your
// install script, assumed yes. Off a terminal it skipped its own prompt and
// applied.
//
// The rule: on a terminal, ask. Off a terminal, refuse unless --yes was passed,
// because a pipe cannot answer a question.
//
// Callers that already got explicit intent from a flag (restore --force) pass
// assumeYes and are let straight through.
func confirmWrite(w io.Writer, prompt string, assumeYes bool) error {
	return confirmWith(w, prompt, assumeYes, false)
}

// confirmWith is confirmWrite with the answer preselected on the terminal:
// yes for a step the command exists to do, like creating the repository a
// push goes to.
func confirmWith(w io.Writer, prompt string, assumeYes, def bool) error {
	if assumeYes {
		return nil
	}
	if !tui.Interactive() {
		fmt.Fprintf(w, "Refusing to continue without a terminal to confirm on.\n")
		fmt.Fprintf(w, "Re-run with --yes if you meant it, or --dry-run to see what would change.\n")
		return ExitError{Code: 1}
	}
	ok, err := tui.ConfirmDefault(prompt, def)
	if err != nil && !errors.Is(err, tui.ErrAborted) {
		return err
	}
	if !ok {
		fmt.Fprintln(w, "Stopped. Nothing was changed.")
		return ExitError{Code: 1}
	}
	return nil
}
