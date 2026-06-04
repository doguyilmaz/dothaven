package cli

import (
	"context"
	"fmt"
	"os"
	"regexp"

	"github.com/doguyilmaz/dothaven/internal/chezmoi"
	"github.com/doguyilmaz/dothaven/internal/sys"
	"github.com/spf13/cobra"
)

var ageEncryptionRe = regexp.MustCompile(`encryption\s*=\s*"age"`)

func probeInitState(env *sys.OS) chezmoi.InitState {
	ctx := context.Background()
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
			steps := chezmoi.PlanInit(probeInitState(env))

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
			fmt.Println("\nRun the commands above, then re-run `dothaven init`.")
			return nil
		},
	}
}
