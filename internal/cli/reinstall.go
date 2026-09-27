package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/doguyilmaz/dothaven/internal/sys"
	"github.com/doguyilmaz/dothaven/internal/tui"
	"github.com/spf13/cobra"
)

const homebrewInstall = `/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"`

func newReinstallCmd(env *sys.OS) *cobra.Command {
	var dryRun, assumeYes bool
	c := &cobra.Command{
		Use:   "reinstall <backup>",
		Short: "Reinstall the apps & packages a backup recorded",
		Long: "Every backup records what was installed — Homebrew formulae and casks, global\n" +
			"npm/pnpm/bun/pipx/uv/cargo packages, editor extensions — as a script that\n" +
			"installs them again. This runs it, on the terminal so Homebrew and sudo can\n" +
			"ask for your password. Each step skips itself if its tool is missing and\n" +
			"never stops the rest, so it is safe to run again.\n\n" +
			"Use --dry-run to read the script first.",
		Args:          cobra.MaximumNArgs(1),
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := backupArg(env, args, "Reinstall from which backup?")
			if err != nil || path == "" {
				return ignoreAbort(err)
			}
			dir, cleanup, err := openBackup(path)
			defer cleanup()
			if err != nil {
				return err
			}
			if !dryRun {
				if err := confirmWrite(os.Stderr, "Install everything this backup lists?", assumeYes); err != nil {
					return err
				}
			}
			return runReinstall(cmd.Context(), dir, dryRun)
		},
	}
	c.Flags().BoolVar(&dryRun, "dry-run", false, "print the install script, run nothing")
	c.Flags().BoolVar(&assumeYes, "yes", false, "skip the confirmation (required off a terminal)")
	return c
}

// backupArg resolves the backup a command should read: the argument, or on a
// terminal a pick from the ones on disk.
func backupArg(env *sys.OS, args []string, title string) (string, error) {
	if len(args) == 1 {
		return filepath.Abs(args[0])
	}
	if !tui.Interactive() {
		return "", fmt.Errorf("which backup? pass a path to a backup folder or file")
	}
	return pickBackup(env, title)
}

// runReinstall runs a backup's install script attached to this terminal.
//
// No timeout, deliberately: `brew bundle` on a fresh machine is tens of
// minutes of downloads, and a kill at some arbitrary deadline would leave it
// half-installed. Ctrl-C still stops it — the context kills the script.
func runReinstall(ctx context.Context, dir string, dryRun bool) error {
	script := filepath.Join(dir, "inventory", "install-packages.sh")
	body, err := os.ReadFile(script)
	if err != nil {
		return fmt.Errorf("this backup has no install list (it was made without the inventory, or by an older dothaven)")
	}
	if dryRun {
		fmt.Println(string(body))
		return nil
	}
	summariseInstall(string(body))
	if runtime.GOOS == "darwin" && strings.Contains(string(body), "brew bundle") {
		if _, err := exec.LookPath("brew"); err != nil {
			fmt.Println(warn("⚠ Homebrew isn't installed, so the apps and formulae will be skipped."))
			fmt.Printf("  Install it first, then run this again:\n    %s\n\n", kbd(homebrewInstall))
		}
	}
	cmd := exec.CommandContext(ctx, "bash", script)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ExitError{Code: 130}
		}
		return fmt.Errorf("install script failed: %w", err)
	}
	fmt.Println("\n" + good("✓ Done.") + " Anything a tool could not install was skipped; check with " + kbd("dothaven doctor <backup>") + ".")
	return nil
}

// summariseInstall says what is about to happen before minutes of output
// scroll past, counted per tool.
func summariseInstall(script string) {
	counts := map[string]int{}
	var order []string
	inBrew := false
	for _, l := range strings.Split(script, "\n") {
		t := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(t, "brew bundle"):
			inBrew = true
			continue
		case t == "BREWFILE":
			inBrew = false
			continue
		}
		tool := ""
		if inBrew {
			if f := strings.Fields(t); len(f) > 0 && !strings.HasPrefix(t, "#") {
				tool = "Homebrew " + f[0]
			}
		} else if strings.HasSuffix(t, "|| true") {
			tool = strings.Fields(t)[0]
			if tool == "sudo" {
				tool = strings.Fields(t)[1]
			}
		}
		if tool == "" {
			continue
		}
		if counts[tool] == 0 {
			order = append(order, tool)
		}
		counts[tool]++
	}
	if len(order) == 0 {
		return
	}
	var parts []string
	for _, t := range order {
		parts = append(parts, fmt.Sprintf("%s %d", t, counts[t]))
	}
	fmt.Printf("%s %s\n\n", bold("Installing:"), strings.Join(parts, " · "))
}
