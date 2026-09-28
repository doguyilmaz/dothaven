// Package cli wires the dothaven subcommands onto a Cobra root.
package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/doguyilmaz/dothaven/internal/snapshot"
	"github.com/doguyilmaz/dothaven/internal/sys"
	"github.com/doguyilmaz/dothaven/internal/tui"
	"github.com/spf13/cobra"
)

// NewRoot builds the root command with every subcommand wired in.
func NewRoot(env *sys.OS, version string) *cobra.Command {
	// Commands list in the order they are added below — the order of the job —
	// not alphabetically, which put "tui" after "ready".
	cobra.EnableCommandSorting = false
	root := &cobra.Command{
		Use:   "dothaven",
		Short: "Keep your dev setup when you change machines",
		Long: "dothaven finds your dev config — dotfiles, editor and terminal settings, AI tool\n" +
			"skills, agents, plugins and MCP servers, cloud logins, SSH keys — plus the apps\n" +
			"and packages you have installed, and moves them to another machine.\n\n" +
			"Moving to a new machine:\n" +
			"  dothaven ready               1. anything only on this machine? (unpushed work, .env)\n" +
			"  dothaven backup --encrypt    2. everything, keys included, in ONE encrypted file\n" +
			"  dothaven restore <file>      3. on the new machine: put it all back\n" +
			"  dothaven reinstall <file>    4. reinstall your apps & packages\n" +
			"  dothaven missing <file>      5. check what's still missing\n\n" +
			"Or keep it in a private GitHub repo instead of a file:\n" +
			"  dothaven github push         on the old machine (encrypted by default)\n" +
			"  dothaven restore github      on the new one\n\n" +
			"Run `dothaven` with no arguments for a menu that walks you through it.\n" +
			"Anything that changes files you already have asks first and takes --dry-run.",
		Version: version,
		// Subcommand errors are returned via RunE; don't dump usage on them.
		// main prints every error once, as "error: …"; cobra printing it too
		// showed each twice, and an empty "Error:" for a plain exit code.
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
	}
	root.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		return fmt.Errorf("%w — see `%s --help`", err, c.CommandPath())
	})
	// Bare `dothaven` on a terminal opens the menu instead of printing help.
	// Someone who cannot remember which of the verbs they want is exactly the
	// person the menu is for, and help is what they get today. Off a terminal
	// it still prints help, because a pipe cannot drive a menu.
	root.RunE = func(cmd *cobra.Command, _ []string) error {
		if !tui.Interactive() {
			return cmd.Help()
		}
		sub, _, err := cmd.Find([]string{"tui"})
		if err != nil {
			return cmd.Help()
		}
		sub.SetContext(cmd.Context())
		return sub.RunE(sub, nil)
	}
	// Grouped, because a flat list of eighteen verbs is the reason this tool
	// reads as complicated. The groups are the jobs someone actually has —
	// set up a machine, save a config, put one back — not the internal shape
	// of the code. Registration lives here so the grouping stays in one place
	// rather than being spread across eighteen constructors.
	root.AddGroup(
		&cobra.Group{ID: "start", Title: "Start here:"},
		&cobra.Group{ID: "save", Title: "Save this machine:"},
		&cobra.Group{ID: "apply", Title: "Set up a machine:"},
		&cobra.Group{ID: "inspect", Title: "Look, without changing anything:"},
		&cobra.Group{ID: "secrets", Title: "Secrets:"},
		&cobra.Group{ID: "sync", Title: "Keep it in a private GitHub repo:"},
		&cobra.Group{ID: "chezmoi", Title: "Sync through a chezmoi repo (optional):"},
	)
	add := func(group string, cmds ...*cobra.Command) {
		for _, c := range cmds {
			c.GroupID = group
			root.AddCommand(c)
		}
	}
	add("start", newTUICmd(env), newUICmd(env, version), newGuideCmd(env), newReadyCmd(env))
	add("save",
		newBackupCmd(env), newIncludeCmd(env), newCollectCmd(env),
		newDefaultsCmd(env), newServicesCmd(env))
	add("apply", newRestoreCmd(env), newReinstallCmd(env))
	add("sync", newGitHubCmd(env))
	add("inspect",
		newStatusCmd(env), newDiffCmd(env), newMissingCmd(env), newCheckCmd(env), newDoctorCmd(env, version),
		newCompareCmd(env), newListCmd(env))
	add("secrets", newScanCmd(env), newSecurityCmd(env))
	add("chezmoi", newInitCmd(env), newChezmoiExportCmd(env), newMigrateCmd(env))
	// Ungrouped, so it lands under "Additional Commands" beside help and
	// completion: it is about the tool, not about anything on this machine.
	root.AddCommand(newUpgradeCmd(env, version))

	return root
}

// Execute builds the command tree, runs it, and prints a pending update notice
// afterwards.
//
// The check runs alongside the command rather than before it, so on anything
// that does real work it costs nothing at all. The notice prints after the
// output, not ahead of it: "here is what to do about this" belongs at the end,
// where the reader already is.
func Execute(ctx context.Context, env *sys.OS, version string) error {
	takeSecretEnv()
	root := NewRoot(env, version)
	var probe *updateProbe
	if !suppressNotice(root, os.Args[1:]) {
		probe = startUpdateCheck(ctx, env, version)
	}
	err := root.ExecuteContext(ctx)
	probe.finish(os.Stderr, version)
	return err
}

// ExitError carries a desired process exit code without a printed message. A
// drift/parity failure is a normal CI outcome, not an error to surface — main
// maps it straight to os.Exit.
type ExitError struct{ Code int }

func (e ExitError) Error() string { return "" }

// --- shared helpers ---

func cwd() string {
	d, _ := os.Getwd()
	return d
}

// stdoutIsTTY reports whether stdout is a terminal (for color), with no deps.
func stdoutIsTTY() bool {
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// stderrIsTTY reports whether stderr is a terminal — gates progress output so a
// piped/CI run never gets carriage-return spam.
func stderrIsTTY() bool {
	fi, err := os.Stderr.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// outputDir is where a command writes when not told otherwise: always
// dothaven's data directory. It used to be ./reports whenever the current
// directory was a git repository, which put a snapshot of your machine inside
// whatever project you happened to be in — one `git add .` from being pushed —
// and let the commands that read snapshots look somewhere else.
func outputDir(env *sys.OS, explicit string) string {
	if explicit != "" {
		return explicit
	}
	return env.DataDir()
}

// snapshotDir is where collect writes snapshots.
func snapshotDir(env *sys.OS) string { return filepath.Join(env.DataDir(), "snapshots") }

// newestSnapshots returns up to n snapshots, newest first, from every place one
// can be: the snapshot folder, and the two older locations (the data directory
// itself, and ./reports) so snapshots from earlier versions are still found.
func newestSnapshots(env *sys.OS, n int) []string {
	type fe struct {
		path string
		mod  time.Time
	}
	var all []fe
	seen := map[string]bool{}
	for _, dir := range []string{snapshotDir(env), env.DataDir(), filepath.Join(cwd(), "reports")} {
		for _, p := range newestJSON(dir, n) {
			if seen[p] {
				continue
			}
			seen[p] = true
			if fi, err := os.Stat(p); err == nil {
				all = append(all, fe{p, fi.ModTime()})
			}
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].mod.After(all[j].mod) })
	var out []string
	for i := 0; i < len(all) && i < n; i++ {
		out = append(out, all[i].path)
	}
	return out
}

// newestJSON returns up to n .json files in dir, newest (by mtime) first.
func newestJSON(dir string, n int) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	type fe struct {
		path string
		mod  time.Time
	}
	var files []fe
	for _, e := range entries {
		// applied.json is an older version's restore ledger, not a snapshot.
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || e.Name() == "applied.json" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fe{filepath.Join(dir, e.Name()), info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })
	var out []string
	for i := 0; i < len(files) && i < n; i++ {
		out = append(out, files[i].path)
	}
	return out
}

func parseSnapshotFile(env *sys.OS, path string) (snapshot.Snapshot, error) {
	b, err := env.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return snapshot.Parse(b)
}

// label is a snapshot file's basename without the .json extension.
func label(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ".json")
}

func sortedStringKeys(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func sortStrings(s []string) { sort.Strings(s) }
