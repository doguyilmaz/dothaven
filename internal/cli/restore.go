package cli

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/doguyilmaz/dothaven/internal/registry"
	"github.com/doguyilmaz/dothaven/internal/restore"
	"github.com/doguyilmaz/dothaven/internal/sys"
	"github.com/doguyilmaz/dothaven/internal/tui"
	"github.com/spf13/cobra"
)

func targetsFor(env *sys.OS) []registry.BackupTarget {
	return registry.BackupTargets(env.Home(), registry.Entries)
}

// conflictAction maps a TUI choice onto the restore engine's action enum, keeping
// the tui and restore packages decoupled (cli is the adapter).
func conflictAction(c tui.ConflictChoice) restore.ConflictAction {
	switch c {
	case tui.ChoiceOverwrite:
		return restore.ActionOverwrite
	case tui.ChoiceOverwriteAll:
		return restore.ActionOverwriteAll
	case tui.ChoiceSkipAll:
		return restore.ActionSkipAll
	default:
		return restore.ActionSkip
	}
}

func newRestoreCmd(env *sys.OS) *cobra.Command {
	var dryRun, force bool
	var only, skip []string
	c := &cobra.Command{
		Use:   "restore <backup-path>",
		Short: "Restore files from a backup into your home directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			backupPath, _ := filepath.Abs(args[0])
			plan, err := restore.BuildPlan(backupPath, env.Home(), targetsFor(env))
			if err != nil {
				return err
			}
			plan = restore.Filter(plan, only, skip)
			if len(plan.Entries) == 0 {
				fmt.Println("No restorable files found in backup.")
				return nil
			}

			if dryRun {
				printRestorePlan(plan)
				return nil
			}

			// Interactive per-conflict resolution on a terminal (unless --force,
			// which overwrites all). Piped/CI runs stay at the safe default: skip
			// conflicts.
			interactive := !force && tui.Interactive()
			snapDir := ""
			if force || interactive {
				snapDir = filepath.Join(env.ResolveOutputDir(""), "pre-restore-"+sys.Timestamp(time.Now()))
			}
			opts := restore.ExecuteOptions{Force: force, SnapshotDir: snapDir}
			if interactive {
				opts.Resolve = func(e restore.Entry, backup, live string) restore.ConflictAction {
					choice, err := tui.ResolveConflict(e.TargetPath, backup, live)
					if err != nil {
						return restore.ActionSkip
					}
					return conflictAction(choice)
				}
			}
			res, err := restore.Execute(plan, opts)
			if err != nil {
				return err
			}

			if res.SnapshotDir != "" {
				fmt.Printf("Pre-restore snapshot saved to: %s\n", res.SnapshotDir)
			}
			if res.Restored == 0 {
				skipped := restore.Tally(plan.Entries).Conflict
				if !force && skipped > 0 {
					fmt.Printf("No files restored. %d conflict(s) skipped — re-run with --force to overwrite.\n", skipped)
				} else {
					fmt.Println("No files restored (everything already up to date).")
				}
				return nil
			}
			fmt.Printf("Restored %d file(s) across: %s\n", res.Restored, formatCategories(res.PerCategory))
			if res.Skipped > 0 {
				fmt.Printf("  %d file(s) skipped\n", res.Skipped)
			}
			if res.SkippedSymlink > 0 {
				fmt.Printf("  %d symlinked target(s) skipped — resolve manually so we don't write through a link\n", res.SkippedSymlink)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&dryRun, "dry-run", false, "show what would change without writing")
	c.Flags().BoolVar(&force, "force", false, "overwrite differing files (a pre-restore snapshot is saved first)")
	c.Flags().StringSliceVar(&only, "only", nil, "only these categories (comma-separated)")
	c.Flags().StringSliceVar(&skip, "skip", nil, "skip these categories (comma-separated)")
	return c
}

var restoreStatusLabel = map[restore.Status]string{
	restore.StatusNew:      "[NEW]     ",
	restore.StatusConflict: "[CONFLICT]",
	restore.StatusSame:     "[SAME]    ",
	restore.StatusRedacted: "[REDACTED]",
}

func printRestorePlan(plan restore.Plan) {
	fmt.Print("\nDry run — no files will be changed:\n\n")
	entries := append([]restore.Entry(nil), plan.Entries...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].BackupPath < entries[j].BackupPath })
	for _, e := range entries {
		fmt.Printf("  %s %s → %s\n", restoreStatusLabel[e.Status], e.BackupPath, e.TargetPath)
	}
	t := restore.Tally(plan.Entries)
	var parts []string
	if t.New > 0 {
		parts = append(parts, fmt.Sprintf("%d new", t.New))
	}
	if t.Conflict > 0 {
		parts = append(parts, fmt.Sprintf("%d conflicts", t.Conflict))
	}
	if t.Same > 0 {
		parts = append(parts, fmt.Sprintf("%d unchanged", t.Same))
	}
	if t.Redacted > 0 {
		parts = append(parts, fmt.Sprintf("%d redacted (skipped)", t.Redacted))
	}
	fmt.Printf("\n  %d files total: %s\n", len(plan.Entries), strings.Join(parts, ", "))
}

func newStatusCmd(env *sys.OS) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Summarize the latest backup against the live machine",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			backupDir := latestBackup(env.ResolveOutputDir(""))
			if backupDir == "" {
				fmt.Println("No backup found. Run 'dothaven backup' first.")
				return nil
			}
			plan, err := restore.BuildPlan(backupDir, env.Home(), targetsFor(env))
			if err != nil {
				return err
			}
			t := restore.Tally(plan.Entries)
			fmt.Printf("Last backup: %s (%s)\n", backupAge(backupDir), filepath.Base(backupDir))
			fmt.Printf("  %d files tracked: %d modified, %d unchanged\n", len(plan.Entries), t.Conflict, t.Same)
			if t.New > 0 {
				fmt.Printf("  %d not on machine (new in backup)\n", t.New)
			}
			if t.Redacted > 0 {
				fmt.Printf("  %d redacted\n", t.Redacted)
			}
			if t.Conflict > 0 {
				fmt.Println("\nModified since backup:")
				mods := make([]string, 0, t.Conflict)
				for _, e := range plan.Entries {
					if e.Status == restore.StatusConflict {
						mods = append(mods, e.BackupPath)
					}
				}
				sort.Strings(mods)
				for _, m := range mods {
					fmt.Printf("  %s\n", m)
				}
			}
			if t.Conflict == 0 && t.New == 0 {
				fmt.Println("\nEverything up to date.")
			}
			return nil
		},
	}
}

var diffStatusColor = map[restore.Status]string{
	restore.StatusConflict: "\x1b[33m", // yellow
	restore.StatusNew:      "\x1b[34m", // blue
	restore.StatusSame:     "\x1b[32m", // green
	restore.StatusRedacted: "\x1b[90m", // gray
}

var diffStatusLabel = map[restore.Status]string{
	restore.StatusConflict: "modified",
	restore.StatusNew:      "new in backup (missing on machine)",
	restore.StatusSame:     "unchanged",
	restore.StatusRedacted: "redacted",
}

func newDiffCmd(env *sys.OS) *cobra.Command {
	var section string
	c := &cobra.Command{
		Use:   "diff [backup-path]",
		Short: "Compare a backup against the live machine, grouped by category",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			backupDir := ""
			if len(args) > 0 {
				backupDir, _ = filepath.Abs(args[0])
			} else {
				backupDir = latestBackup(env.ResolveOutputDir(""))
			}
			if backupDir == "" {
				fmt.Println("No backup found. Run 'dothaven backup' first.")
				return nil
			}
			plan, err := restore.BuildPlan(backupDir, env.Home(), targetsFor(env))
			if err != nil {
				return err
			}
			entries := plan.Entries
			if section != "" {
				entries = restore.Filter(plan, []string{section}, nil).Entries
				if len(entries) == 0 {
					fmt.Printf("No entries found for section: %s\n", section)
					return nil
				}
			}

			tty := stdoutIsTTY()
			reset := ""
			if tty {
				reset = "\x1b[0m"
			}
			colorize := func(s restore.Status, text string) string {
				if !tty {
					return text
				}
				return diffStatusColor[s] + text + reset
			}

			byCat := map[string][]restore.Entry{}
			for _, e := range entries {
				byCat[e.Category] = append(byCat[e.Category], e)
			}
			cats := make([]string, 0, len(byCat))
			for c := range byCat {
				cats = append(cats, c)
			}
			sort.Strings(cats)

			fmt.Print("\nComparing backup against live system:\n\n")
			for _, cat := range cats {
				fmt.Printf("  %s/\n", cat)
				ents := byCat[cat]
				sort.Slice(ents, func(i, j int) bool { return ents[i].BackupPath < ents[j].BackupPath })
				for _, e := range ents {
					fmt.Println(colorize(e.Status, fmt.Sprintf("  %s — %s", e.BackupPath, diffStatusLabel[e.Status])))
				}
			}

			t := restore.Tally(entries)
			var parts []string
			if t.Conflict > 0 {
				parts = append(parts, colorize(restore.StatusConflict, fmt.Sprintf("%d modified", t.Conflict)))
			}
			if t.Same > 0 {
				parts = append(parts, colorize(restore.StatusSame, fmt.Sprintf("%d unchanged", t.Same)))
			}
			if t.New > 0 {
				parts = append(parts, colorize(restore.StatusNew, fmt.Sprintf("%d new", t.New)))
			}
			if t.Redacted > 0 {
				parts = append(parts, colorize(restore.StatusRedacted, fmt.Sprintf("%d redacted", t.Redacted)))
			}
			fmt.Printf("\n  %d files: %s\n", len(entries), strings.Join(parts, ", "))
			return nil
		},
	}
	c.Flags().StringVar(&section, "section", "", "only show this category")
	return c
}
