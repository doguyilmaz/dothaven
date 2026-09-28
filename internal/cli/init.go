package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"filippo.io/age"

	"github.com/doguyilmaz/dothaven/internal/chezmoi"
	"github.com/doguyilmaz/dothaven/internal/sys"
	"github.com/doguyilmaz/dothaven/internal/tui"
	"github.com/spf13/cobra"
)

// runShown runs a command and echoes it and its output, for the guided init.
func runShown(ctx context.Context, name string, args ...string) {
	fmt.Printf("  $ %s %s\n", name, strings.Join(args, " "))
	out, err := runShell(ctx, name, args...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %s %v\n%s\n", danger("✗"), err, out)
		return
	}
	if out != "" {
		fmt.Println(out)
	}
	fmt.Printf("  %s done\n", good("✓"))
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
	_, keyErr := os.Stat(env.Home() + "/.config/chezmoi/key.txt")

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
		AgeKeyExists:      keyErr == nil,
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

			fmt.Print("dothaven init: chezmoi + age setup\n\n")
			printInitSteps(steps)
			if chezmoi.IsReady(steps) {
				printInitReady()
				return nil
			}
			if !tui.Interactive() {
				fmt.Printf("\nRun the commands above, then run %s again.\n", kbd("dothaven init"))
				return nil
			}

			// Guided: offer the safe steps. The age key itself is never made
			// here: it is yours to create and back up. Once it exists, adding it
			// to chezmoi.toml is safe, and without that the step never passes.
			fmt.Println()
			for _, s := range steps {
				if s.Done {
					continue
				}
				switch s.ID {
				case "chezmoi":
					if ok, err := tui.Confirm("Install chezmoi with Homebrew now?"); err != nil {
						return ignoreAbort(err)
					} else if ok {
						runShown(ctx, "brew", "install", "chezmoi")
					}
				case "age-key":
					if !state.AgeKeyExists {
						fmt.Println("  Make the age key with the command above and back it up, then run init again.")
						continue
					}
					if ok, err := tui.Confirm("Add your age key to ~/.config/chezmoi/chezmoi.toml now?"); err != nil {
						return ignoreAbort(err)
					} else if ok {
						if err := writeAgeConfig(env); err != nil {
							fmt.Fprintf(os.Stderr, "  %s %v\n", danger("✗"), err)
						} else {
							fmt.Printf("  %s chezmoi.toml now uses your age key.\n", good("✓"))
						}
					}
				case "source":
					if !probeInitState(ctx, env).ChezmoiInstalled {
						fmt.Println("  chezmoi is not installed yet, so the repository step waits for the next run.")
						continue
					}
					url, err := tui.Input("Private repo URL", chezmoi.RepoURL(state.User))
					if err != nil {
						return ignoreAbort(err)
					}
					if strings.Contains(url, "<you>") {
						fmt.Println("  Set your repo URL, or run: chezmoi init <url>")
					} else if ok, err := tui.Confirm("Run `chezmoi init " + url + "`?"); err != nil {
						return ignoreAbort(err)
					} else if ok {
						runShown(ctx, "chezmoi", "init", url)
					}
				}
			}

			steps = chezmoi.PlanInit(probeInitState(ctx, env))
			fmt.Println("\nNow:")
			printInitSteps(steps)
			if chezmoi.IsReady(steps) {
				printInitReady()
			} else {
				fmt.Printf("\nWhen every step shows %s, run: %s\n", good("✓"), kbd("dothaven chezmoi-export"))
			}
			return nil
		},
	}
}

func printInitSteps(steps []chezmoi.InitStep) {
	for _, s := range steps {
		if s.Done {
			fmt.Printf("  %s %s\n", good("✓"), s.Title)
			continue
		}
		fmt.Printf("  %s %s\n", warn("→"), s.Title)
		if s.Command != "" {
			fmt.Printf("      %s\n", kbd(s.Command))
		}
		if s.Note != "" {
			fmt.Printf("      %s\n", dim(s.Note))
		}
	}
}

func printInitReady() {
	fmt.Printf("\n%s Setup complete. Next:\n  %s          %s\n  %s  %s\n", good("✓"),
		kbd("dothaven chezmoi-export"), dim("# shows the plan, then asks"),
		kbd("dothaven chezmoi-export --apply"), dim("# carries it out"))
}

// writeAgeConfig points chezmoi at the age key: encryption = "age" plus an
// [age] table with the key file and its public recipient. The old file is
// kept next to it.
func writeAgeConfig(env *sys.OS) error {
	dir := filepath.Join(env.Home(), ".config", "chezmoi")
	keyFile, tomlFile := filepath.Join(dir, "key.txt"), filepath.Join(dir, "chezmoi.toml")
	kb, err := os.ReadFile(keyFile)
	if err != nil {
		return err
	}
	ids, err := age.ParseIdentities(bytes.NewReader(kb))
	if err != nil {
		return fmt.Errorf("%s is not an age key: %w", shortHome(env, keyFile), err)
	}
	var recipient string
	for _, id := range ids {
		if x, ok := id.(*age.X25519Identity); ok {
			recipient = x.Recipient().String()
			break
		}
	}
	if recipient == "" {
		return fmt.Errorf("%s holds no X25519 key; add the [age] settings by hand", shortHome(env, keyFile))
	}
	old, err := os.ReadFile(tomlFile)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	cfg, ok := chezmoi.AgeConfig(string(old), keyFile, recipient)
	if !ok {
		return fmt.Errorf("%s already sets encryption or [age]; check it by hand", shortHome(env, tomlFile))
	}
	if len(old) > 0 {
		if err := sys.WriteFileSecure(tomlFile+".before-dothaven", string(old)); err != nil {
			return err
		}
	}
	return sys.WriteFileSecure(tomlFile, cfg)
}
