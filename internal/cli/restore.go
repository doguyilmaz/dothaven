package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/doguyilmaz/dothaven/internal/registry"
	"github.com/doguyilmaz/dothaven/internal/restore"
	"github.com/doguyilmaz/dothaven/internal/sys"
	"github.com/doguyilmaz/dothaven/internal/tui"
	"github.com/spf13/cobra"
)

// restoreTargets maps a backup back onto this machine: the registry, the
// user's includes, and a catch-all for extra/ so paths someone included on the
// old machine come back even before this one has an include list.
func restoreTargets(env *sys.OS) []registry.BackupTarget {
	t := registry.BackupTargets(env.Home(), allEntries(env))
	return append(t, registry.BackupTarget{
		Src: env.Home(), Dest: registry.ExtraCategory, Category: registry.ExtraCategory,
		IsDir: true, Sensitivity: registry.Medium,
	})
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
	var dryRun, force, assumeYes bool
	var only, skip []string
	c := &cobra.Command{
		Use:   "restore [backup]",
		Short: "Put a backup's files back into your home folder",
		Long: "Accepts a backup folder, a .tar.gz, or an encrypted .tar.gz.age (asks for the\n" +
			"passphrase; no other tools needed). With no path on a terminal, it lists the\n" +
			"backups it can find — dothaven's folder, Downloads, Desktop, USB drives.\n\n" +
			"New files are written. A file that already exists and differs is a conflict:\n" +
			"on a terminal you choose per file (with a diff); otherwise it is kept, unless\n" +
			"--force. Anything overwritten is saved to a pre-restore snapshot first.\n\n" +
			"Afterwards it offers the rest: your macOS settings, and reinstalling apps.",
		Args:          cobra.MaximumNArgs(1),
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := ""
			if len(args) == 1 {
				path, _ = filepath.Abs(args[0])
			} else {
				if !tui.Interactive() {
					return fmt.Errorf("which backup? pass a path: dothaven restore <folder|file>")
				}
				p, err := pickBackup(env, "Which backup should be restored?")
				if err != nil || p == "" {
					return ignoreAbort(err)
				}
				path = p
			}
			return runRestore(cmd, env, path, restoreOpts{dryRun: dryRun, force: force, yes: assumeYes, only: only, skip: skip})
		},
	}
	c.Flags().BoolVar(&dryRun, "dry-run", false, "show what would change without writing")
	c.Flags().BoolVar(&force, "force", false, "overwrite differing files (a pre-restore snapshot is saved first)")
	c.Flags().BoolVar(&assumeYes, "yes", false, "don't ask before writing")
	c.Flags().StringSliceVar(&only, "only", nil, "only these categories (comma-separated)")
	c.Flags().StringSliceVar(&skip, "skip", nil, "skip these categories (comma-separated)")
	return c
}

type restoreOpts struct {
	dryRun, force, yes bool
	only, skip         []string
}

// ignoreAbort treats Esc / Ctrl-C at a prompt as "never mind".
func ignoreAbort(err error) error {
	if errors.Is(err, tui.ErrAborted) {
		return nil
	}
	return err
}

func runRestore(cmd *cobra.Command, env *sys.OS, path string, o restoreOpts) error {
	dir, cleanup, err := openBackup(path)
	defer cleanup()
	if err != nil {
		return err
	}
	plan, err := restore.BuildPlan(dir, env.Home(), restoreTargets(env))
	if err != nil {
		return err
	}
	plan = restore.Filter(plan, o.only, o.skip)
	extras := backupExtras(dir)
	if len(plan.Entries) == 0 && !extras.any() {
		fmt.Println("No restorable files found in that backup.")
		return nil
	}

	fmt.Printf("%s %s\n", bold("Backup:"), shortHome(env, path))
	if m := manifestLine(dir); m != "" {
		fmt.Printf("  %s\n", dim(m))
	}
	t := restore.Tally(plan.Entries)
	fmt.Printf("  %s: %s\n", plural(len(plan.Entries), "file"), restoreBreakdown(t))

	if o.dryRun {
		printRestorePlan(plan)
		printRedacted(plan)
		printNextSteps(env, path, extras)
		return nil
	}
	if t.New+t.Conflict == 0 {
		fmt.Println(good("✓ Every file in the backup already matches this machine."))
		printRedacted(plan)
		return offerExtras(cmd, env, path, dir, extras)
	}

	interactive := !o.force && tui.Interactive()
	if interactive && !o.yes {
		q := fmt.Sprintf("Write %s into your home folder?", plural(t.New, "new file"))
		if t.Conflict > 0 {
			q = fmt.Sprintf("Write %s, and ask about the %s that differ?", plural(t.New, "new file"), plural(t.Conflict, "file"))
		}
		ok, err := tui.Confirm(q)
		if err != nil || !ok {
			fmt.Println("Nothing written.")
			return ignoreAbort(err)
		}
	}

	snapDir := ""
	if o.force || interactive {
		snapDir = filepath.Join(env.DataDir(), "pre-restore-"+sys.Timestamp(time.Now()))
	}
	opts := restore.ExecuteOptions{Force: o.force, SnapshotDir: snapDir}
	if interactive {
		opts.Resolve = func(e restore.Entry, backup, live string) restore.ConflictAction {
			choice, err := tui.ResolveConflict(shortHome(env, e.TargetPath), backup, live)
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

	if res.Restored > 0 {
		fmt.Printf("\n%s %s %s\n", good("✓"), bold(fmt.Sprintf("Restored %s", plural(res.Restored, "file"))), dim("— "+formatCategories(res.PerCategory)))
	} else {
		fmt.Println("\nNo files restored.")
	}
	if skipped := t.Conflict - (res.Restored - t.New); skipped > 0 && !o.force {
		fmt.Printf("  %s %s kept as they are on this machine", dim("•"), plural(skipped, "differing file"))
		if !interactive {
			fmt.Print(" — re-run with --force to overwrite")
		}
		fmt.Println(".")
	}
	if res.SnapshotDir != "" {
		fmt.Printf("  %s overwritten files saved first to %s\n", dim("•"), shortHome(env, res.SnapshotDir))
	}
	if res.SkippedSymlink > 0 {
		fmt.Printf("  %s %s skipped: the file here is a symlink, and writing through it would change what it points to\n", warn("⚠"), plural(res.SkippedSymlink, "file"))
	}
	printRedacted(plan)
	return offerExtras(cmd, env, path, dir, extras)
}

func restoreBreakdown(t restore.Counts) string {
	var parts []string
	if t.New > 0 {
		parts = append(parts, fmt.Sprintf("%d new", t.New))
	}
	if t.Conflict > 0 {
		parts = append(parts, warn(fmt.Sprintf("%d differ from this machine", t.Conflict)))
	}
	if t.Same > 0 {
		parts = append(parts, dim(fmt.Sprintf("%d already the same", t.Same)))
	}
	if t.Redacted > 0 {
		parts = append(parts, dim(fmt.Sprintf("%d redacted", t.Redacted)))
	}
	if len(parts) == 0 {
		return "nothing to do"
	}
	return strings.Join(parts, ", ")
}

// manifestLine is the one line of a backup's MANIFEST worth repeating: where
// and when it was made, and what kind it is.
func manifestLine(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, "MANIFEST.txt"))
	if err != nil {
		return ""
	}
	var host, created, contents string
	for _, l := range strings.Split(string(b), "\n") {
		l = strings.TrimSpace(strings.TrimPrefix(l, "#"))
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "host":
			host = strings.TrimSpace(v)
		case "created":
			created = strings.TrimSpace(v)
		case "contents", "redacted":
			contents = strings.TrimSpace(v)
		}
	}
	if t, err := time.Parse(time.RFC3339, created); err == nil {
		created = t.Format("2 Jan 2006 15:04")
	}
	return strings.TrimSpace(fmt.Sprintf("from %s, %s — %s", host, created, contents))
}

// printRedacted names the files a plaintext backup could not give back.
// Counting them is not enough: "3 redacted" says nothing about which three
// config files the new machine is now missing.
func printRedacted(plan restore.Plan) {
	var red []string
	for _, e := range plan.Entries {
		if e.Status == restore.StatusRedacted {
			red = append(red, e.BackupPath)
		}
	}
	if len(red) == 0 {
		return
	}
	fmt.Printf("\n%s %s\n", warn("⚠"), bold(fmt.Sprintf("%s had secrets redacted, so they were not restored:", plural(len(red), "file"))))
	printList(red, 10)
	fmt.Println(dim("  They are in the backup for reference. For the real thing, restore an encrypted"))
	fmt.Println(dim("  backup (dothaven backup --encrypt on the old machine), or copy the values by hand."))
}

// extras is what a backup carries beyond files.
type extras struct{ prefs, reinstall, inventory bool }

func (e extras) any() bool { return e.prefs || e.reinstall || e.inventory }

func backupExtras(dir string) extras {
	exists := func(rel string) bool { _, err := os.Stat(filepath.Join(dir, rel)); return err == nil }
	return extras{
		prefs:     exists(filepath.Join("macos-defaults", prefsFileName)),
		reinstall: exists(filepath.Join("inventory", "install-packages.sh")),
		inventory: exists(filepath.Join("inventory", "snapshot.json")),
	}
}

func printNextSteps(env *sys.OS, path string, x extras) {
	if !x.any() {
		return
	}
	p := shortHome(env, path)
	fmt.Println("\n" + bold("Also in this backup:"))
	if x.prefs {
		fmt.Printf("  %s  %s\n", kbd("dothaven defaults import "+p), dim("# macOS settings"))
	}
	if x.reinstall {
		fmt.Printf("  %s  %s\n", kbd("dothaven reinstall "+p), dim("# apps & packages you had"))
	}
	if x.inventory {
		fmt.Printf("  %s  %s\n", kbd("dothaven doctor "+p), dim("# what is still missing here"))
	}
}

// offerExtras finishes a restore: on a terminal it offers the settings and the
// reinstall right away, from the backup already open (an encrypted one is not
// decrypted a second time); otherwise it prints the commands.
func offerExtras(cmd *cobra.Command, env *sys.OS, path, dir string, x extras) error {
	if !tui.Interactive() || !x.any() {
		printNextSteps(env, path, x)
		return nil
	}
	ctx := cmd.Context()
	if x.prefs && runtime.GOOS == "darwin" {
		fmt.Println()
		if ok, err := tui.Confirm("Also put back your macOS settings (trackpad, keyboard, Dock, Finder)?"); err == nil && ok {
			if err := importDefaults(ctx, env, dir, false, true, false); err != nil {
				fmt.Fprintln(os.Stderr, "  "+danger("✗")+" "+err.Error())
			}
		}
	}
	if x.reinstall {
		fmt.Println()
		if ok, err := tui.Confirm("Reinstall your apps & packages now? (runs Homebrew etc.; can take a while)"); err == nil && ok {
			if err := runReinstall(ctx, dir, false); err != nil {
				fmt.Fprintln(os.Stderr, "  "+danger("✗")+" "+err.Error())
			}
		} else {
			fmt.Printf("  Later: %s\n", kbd("dothaven reinstall "+shortHome(env, path)))
		}
	}
	if x.inventory {
		fmt.Printf("\nCheck what's still missing any time: %s\n", kbd("dothaven doctor "+shortHome(env, path)))
	}
	return nil
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
		Short: "Latest backup vs this machine — one-screen summary",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			backupDir := latestBackup(env.DataDir())
			if backupDir == "" {
				fmt.Printf("No backup found in %s. Run %s first.\n", dim(env.DataDir()), kbd("dothaven backup"))
				return nil
			}
			plan, err := restore.BuildPlan(backupDir, env.Home(), restoreTargets(env))
			if err != nil {
				return err
			}
			t := restore.Tally(plan.Entries)
			fmt.Printf("%s %s %s\n", bold("Last backup:"), backupAge(backupDir), dim("("+filepath.Base(backupDir)+")"))
			modified := fmt.Sprintf("%d modified", t.Conflict)
			if t.Conflict > 0 {
				modified = warn(modified)
			}
			fmt.Printf("  %d files tracked: %s, %s\n", len(plan.Entries), modified, dim(fmt.Sprintf("%d unchanged", t.Same)))
			if t.New > 0 {
				fmt.Println(dim(fmt.Sprintf("  %d not on machine (new in backup)", t.New)))
			}
			if t.Redacted > 0 {
				fmt.Println(dim(fmt.Sprintf("  %d redacted", t.Redacted)))
			}
			if t.Conflict > 0 {
				fmt.Println("\n" + bold("Modified since backup:"))
				mods := make([]string, 0, t.Conflict)
				for _, e := range plan.Entries {
					if e.Status == restore.StatusConflict {
						mods = append(mods, e.BackupPath)
					}
				}
				sort.Strings(mods)
				for _, m := range mods {
					fmt.Printf("  %s %s\n", warn("~"), m)
				}
				fmt.Printf("\n%s\n", dim("These differ from the backup. Run "+kbd("dothaven backup")+" to capture them."))
			}
			if t.Conflict == 0 && t.New == 0 {
				fmt.Println("\n" + good("✓ Everything up to date."))
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
		Short: "Backup vs this machine — file by file",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			backupDir := ""
			if len(args) > 0 {
				backupDir, _ = filepath.Abs(args[0])
			} else {
				backupDir = latestBackup(env.DataDir())
			}
			if backupDir == "" {
				fmt.Printf("No backup found in %s. Run %s first.\n", dim(env.DataDir()), kbd("dothaven backup"))
				return nil
			}
			dir, cleanup, err := openBackup(backupDir)
			defer cleanup()
			if err != nil {
				return err
			}
			plan, err := restore.BuildPlan(dir, env.Home(), restoreTargets(env))
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
