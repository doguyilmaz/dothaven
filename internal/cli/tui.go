package cli

import (
	"fmt"

	"github.com/doguyilmaz/dothaven/internal/sys"
	"github.com/doguyilmaz/dothaven/internal/tui"
	"github.com/spf13/cobra"
)

// newTUICmd is the dedicated interactive launcher: a menu that dispatches to a
// sibling command run with default flags (so each one's own interactive-on-TTY
// flow kicks in). The single-command interactive behavior lives in those
// commands; this is just a discoverable front door.
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
			action, err := tui.MainMenu()
			if err != nil {
				return err
			}
			if action == "" || action == "quit" {
				return nil
			}
			sub, _, ferr := cmd.Root().Find([]string{action})
			if ferr != nil || sub == nil {
				return ferr
			}
			// Calling RunE directly bypasses cobra's lifecycle, including context
			// propagation — without this the sub-command's cmd.Context() is nil and
			// anything deriving a timeout from it panics. Pass our (signal-aware) ctx.
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
		},
	}
	return c
}
