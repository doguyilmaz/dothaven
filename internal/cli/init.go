package cli

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/doguyilmaz/dothaven/internal/chezmoi"
	"github.com/doguyilmaz/dothaven/internal/sys"
	"github.com/doguyilmaz/dothaven/internal/tui"
	"github.com/spf13/cobra"
)

// runShown runs a command, echoing it and its output — used by the guided init.
func runShown(ctx context.Context, name string, args ...string) {
	fmt.Printf("  $ %s %s\n", name, strings.Join(args, " "))
	out, err := runShell(ctx, name, args...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ✗ %v\n%s\n", err, out)
		return
	}
	if out != "" {
		fmt.Println(out)
	}
	fmt.Println("  ✔ done")
}

var ageEncryptionRe = regexp.MustCompile(`encryption\s*=\s*"age"`)

func probeInitState(ctx context.Context, env *sys.OS) chezmoi.InitState {
	nonEmpty := func(name string, args ...string) bool {
		out, err := runShell(ctx, name, args...)
		return err == nil && out != ""
	}

	chezmoiInstalled := nonEmpty("chezmoi", "--version")

	// age is configured when chezmoi.toml declares it.
	ageKeyConfigured := false
	if b, err := os.ReadFile(env.Home() + "/.config/chezmoi/chezmoi.toml"); err == nil {
		ageKeyConfigured = ageEncryptionRe.Match(b)
	}

	// source is initialized when chezmoi reports a path that is a git repo.
	sourceInitialized := false
	if chezmoiInstalled {
		if src, err := runShell(ctx, "chezmoi", "source-path"); err == nil && src != "" {
			if _, e := os.Stat(src + "/.git/HEAD"); e == nil {
				sourceInitialized = true
			}
		}
	}

	user, _ := runShell(ctx, "gh", "api", "user", "--jq", ".login")

	return chezmoi.InitState{
		ChezmoiInstalled:  chezmoiInstalled,
		AgeKeyConfigured:  ageKeyConfigured,
		SourceInitialized: sourceInitialized,
		User:              user,
	}
}

func newInitCmd(env *sys.OS) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Check the chezmoi + age prerequisites for export",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			state := probeInitState(ctx, env)
			steps := chezmoi.PlanInit(state)

			fmt.Print("dothaven init — chezmoi + age bootstrap\n\n")
			for _, s := range steps {
				mark := "→"
				if s.Done {
					mark = "✓"
				}
				fmt.Printf("  %s %s\n", mark, s.Title)
				if !s.Done && s.Command != "" {
					fmt.Printf("      %s\n", s.Command)
				}
				if s.Note != "" {
					fmt.Printf("      ⚠ %s\n", s.Note)
				}
			}

			if chezmoi.IsReady(steps) {
				fmt.Print("\n✓ Setup complete. Next:\n  dothaven chezmoi-export          # dry-run — review the plan\n  dothaven chezmoi-export --apply  # execute\n")
				return nil
			}

			// Non-interactive (piped/CI): just print guidance.
			if !tui.Interactive() {
				fmt.Println("\nRun the commands above, then re-run `dothaven init`.")
				return nil
			}

			// Guided: offer to run the safe steps. The age key is guided-only —
			// generating/placing key material is the user's responsibility.
			fmt.Println()
			for _, s := range steps {
				if s.Done {
					continue
				}
				switch s.ID {
				case "chezmoi":
					if ok, err := tui.Confirm("Install chezmoi via Homebrew now?"); err != nil {
						return err
					} else if ok {
						runShown(ctx, "brew", "install", "chezmoi")
					}
				case "age-key":
					fmt.Println("  → Generate your age key yourself, then re-run init:")
					fmt.Printf("      %s\n", s.Command)
					fmt.Println("    ⚠ Back it up offline — losing it means encrypted files can't be decrypted.")
				case "source":
					fallback := chezmoi.RepoURL(state.User)
					url, err := tui.Input("Private repo URL", fallback)
					if err != nil {
						return err
					}
					if strings.Contains(url, "<you>") {
						fmt.Println("    Set your repo URL and re-run, or run: chezmoi init <url>")
					} else if ok, err := tui.Confirm("Run `chezmoi init " + url + "`?"); err != nil {
						return err
					} else if ok {
						runShown(ctx, "chezmoi", "init", url)
					}
				}
			}
			fmt.Println("\nWhen every step is ✓, run: dothaven chezmoi-export")
			return nil
		},
	}
}
