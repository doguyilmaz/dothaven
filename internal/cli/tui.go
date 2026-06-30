package cli

import (
	"errors"
	"fmt"

	"github.com/doguyilmaz/dothaven/internal/sys"
	"github.com/doguyilmaz/dothaven/internal/tui"
	"github.com/spf13/cobra"
)

// newTUICmd is the dedicated interactive launcher: a menu that dispatches to a
// sibling command run with default flags (so each one's own interactive-on-TTY
// flow kicks in), then returns to the menu — until the user picks Quit (or
// Esc/Ctrl-C). The single-command interactive behavior lives in those commands;
// this is just a discoverable front door.
func newTUICmd(env *sys.OS) *cobra.Command {
	c := &cobra.Command{
		Use:           "tui",
		Short:         "Interactive menu — pick an action",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !tui.Interactive() {
				fmt.Fprintln(cmd.ErrOrStderr(), "the tui command needs an interactive terminal")
				return ExitError{Code: 1}
			}
			for {
				action, err := tui.MainMenu()
				if err != nil {
					return err
				}
				if action == "" || action == "quit" {
					return nil
				}
				if rerr := runTUIAction(cmd, env, action); rerr != nil {
					// An ExitError already conveyed its outcome (e.g. a drift exit
					// code); other errors are shown. Either way, stay in the menu.
					var ee ExitError
					if !errors.As(rerr, &ee) {
						fmt.Fprintln(cmd.ErrOrStderr(), "error:", rerr)
					}
				}
				// A signal cancelled the shared context (Ctrl-C during an action).
				// Leave the menu rather than re-dispatch with a dead context, which
				// would make every subsequent action fail instantly.
				if cmd.Context().Err() != nil {
					return nil
				}
				fmt.Fprintln(cmd.OutOrStdout()) // separate this run's output from the next menu
			}
		},
	}
	return c
}

// runTUIAction dispatches one menu choice to its sibling command. Calling RunE
// directly bypasses cobra's lifecycle (including context propagation), so we set
// the signal-aware context explicitly — without it cmd.Context() is nil and any
// timeout derivation panics.
func runTUIAction(cmd *cobra.Command, env *sys.OS, action string) error {
	sub, _, ferr := cmd.Root().Find([]string{action})
	if ferr != nil || sub == nil {
		return ferr
	}
	sub.SetContext(cmd.Context())
	// restore needs a path argument — feed it the latest backup.
	if action == "restore" {
		latest := latestBackup(env.ResolveOutputDir(""))
		if latest == "" {
			fmt.Println("No backup found. Run a backup first.")
			return nil
		}
		return sub.RunE(sub, []string{latest})
	}
	return sub.RunE(sub, nil)
}
