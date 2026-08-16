// Package cli wires the dothaven subcommands onto a Cobra root.
package cli

import (
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
	root := &cobra.Command{
		Use:   "dothaven",
		Short: "Discover, back up, and migrate your machine's dev config",
		Long: "dothaven inventories your machine's dev config, scans it for secrets, and moves\n" +
			"it to another machine.\n\n" +
			"Two ways to keep a copy — pick by whether it has to leave this Mac:\n" +
			"  backup           a timestamped folder here. Quick, local, no setup.\n" +
			"  chezmoi-export   into your chezmoi repo, secrets age-encrypted. Syncs machines.\n\n" +
			"Run `dothaven` with no arguments for a menu. Anything that changes files you\n" +
			"already have asks first and takes --dry-run; backup and export only ever add.",
		Version: version,
		// Subcommand errors are returned via RunE; don't dump usage on them.
		SilenceUsage:  true,
		SilenceErrors: false,
		Args:          cobra.NoArgs,
	}
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
		&cobra.Group{ID: "save", Title: "Save this machine's config:"},
		&cobra.Group{ID: "apply", Title: "Set up or repair a machine:"},
		&cobra.Group{ID: "inspect", Title: "See what would change (read-only):"},
		&cobra.Group{ID: "secrets", Title: "Secrets:"},
	)
	add := func(group string, cmds ...*cobra.Command) {
		for _, c := range cmds {
			c.GroupID = group
			root.AddCommand(c)
		}
	}
	add("start", newGuideCmd(env), newTUICmd(env), newInitCmd(env))
	add("save",
		newBackupCmd(env), newChezmoiExportCmd(env), newCollectCmd(env),
		newDefaultsCmd(env), newServicesCmd(env))
	add("apply", newMigrateCmd(env), newRestoreCmd(env))
	add("inspect",
		newReadyCmd(env), newStatusCmd(env), newDiffCmd(env), newDoctorCmd(env),
		newCompareCmd(env), newListCmd(env))
	add("secrets", newScanCmd(env), newSecurityCmd(env))

	return root
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
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
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
