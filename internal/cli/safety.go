package cli

import (
	"fmt"
	"io"

	"github.com/doguyilmaz/dothaven/internal/tui"
)

// confirmWrite decides whether a command that changes this machine may run.
//
// One rule in one place, because the commands that write used to answer this
// question five different ways — and `migrate`, which overwrites $HOME and runs
// your install script, was the one that assumed yes. Off a terminal it skipped
// its own prompt and applied.
//
// The rule: on a terminal, ask. Off a terminal, refuse unless --yes was passed.
// A pipe cannot answer a question, and silence is not consent.
//
// Callers that already got explicit intent from a flag (restore --force) pass
// assumeYes and are let straight through.
func confirmWrite(w io.Writer, prompt string, assumeYes bool) error {
	if assumeYes {
		return nil
	}
	if !tui.Interactive() {
		fmt.Fprintf(w, "Refusing to continue without a terminal to confirm on.\n")
		fmt.Fprintf(w, "Re-run with --yes if you meant it, or --dry-run to see what would change.\n")
		return ExitError{Code: 1}
	}
	ok, err := tui.Confirm(prompt)
	if err != nil {
		return err
	}
	if !ok {
		return ExitError{Code: 1}
	}
	return nil
}
