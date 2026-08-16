package cli

import (
	"fmt"
	"os"

	"github.com/doguyilmaz/dothaven/internal/sys"
	"github.com/spf13/cobra"
)

// newMigrateCmd is the clean-machine happy path: one command that checks the
// prerequisites, applies the chezmoi source (which also runs the install
// script), and points at the verification step — so a user on an empty laptop
// doesn't have to remember the multi-command sequence.
func newMigrateCmd(env *sys.OS) *cobra.Command {
	var dryRun, assumeYes bool
	c := &cobra.Command{
		Use:           "migrate",
		Short:         "Set up this machine from your chezmoi source (prereqs → apply → verify)",
		Long:          "On a clean machine: verifies chezmoi + an initialized source repo, applies it\n(pulling configs and running your install script), then points at verification.",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			state := probeInitState(ctx, env)

			if !state.ChezmoiInstalled {
				fmt.Fprintln(os.Stderr, "chezmoi isn't installed. Run `dothaven init` first (brew install chezmoi).")
				return ExitError{Code: 1}
			}
			if !state.SourceInitialized {
				fmt.Fprintln(os.Stderr, "No chezmoi source repo yet. Initialize it first:")
				fmt.Fprintln(os.Stderr, "  chezmoi init <your-private-repo>   (see `dothaven init`)")
				return ExitError{Code: 1}
			}
			if !state.AgeKeyConfigured {
				fmt.Fprintln(os.Stderr, "⚠ age encryption isn't configured — encrypted files won't decrypt on this machine.")
				fmt.Fprintln(os.Stderr, "  Place your age key (see `dothaven init`), or continue to apply non-secret files only.")
			}

			// Show first, write second. `chezmoi diff` is the same comparison
			// chezmoi apply acts on, so this is a real preview rather than a
			// summary that could disagree with what follows.
			if dryRun {
				fmt.Println("Would apply the following (chezmoi diff):")
				out, err := runShell(ctx, "chezmoi", "diff")
				if err != nil {
					fmt.Fprintf(os.Stderr, "✗ chezmoi diff failed: %v\n%s\n", err, out)
					return ExitError{Code: 1}
				}
				if out == "" {
					fmt.Println("  (nothing — this machine already matches your source)")
				} else {
					fmt.Println(out)
				}
				fmt.Println("Re-run without --dry-run to apply.")
				return nil
			}

			// Writes into $HOME and runs your install script.
			if err := confirmWrite(os.Stderr,
				"Apply your chezmoi source to this machine now?", assumeYes); err != nil {
				return err
			}

			fmt.Println("\nApplying chezmoi source… (this also runs your install script)")
			if out, err := runShell(ctx, "chezmoi", "apply"); err != nil {
				fmt.Fprintf(os.Stderr, "✗ chezmoi apply failed: %v\n%s\n", err, out)
				return ExitError{Code: 1}
			} else if out != "" {
				fmt.Println(out)
			}

			fmt.Println("\n✓ Applied. Next:")
			fmt.Println("  chezmoi diff                  # review what's managed")
			fmt.Println("  dothaven doctor <snapshot>    # check installable parity, if you have a snapshot")
			return nil
		},
	}
	c.Flags().BoolVar(&dryRun, "dry-run", false, "show what chezmoi would change, write nothing")
	c.Flags().BoolVar(&assumeYes, "yes", false, "skip the confirmation (required off a terminal)")
	return c
}
