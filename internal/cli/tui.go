package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/doguyilmaz/dothaven/internal/backup"
	"github.com/doguyilmaz/dothaven/internal/sys"
	"github.com/doguyilmaz/dothaven/internal/tui"
	"github.com/spf13/cobra"
)

// machineWord is what the menu calls this computer.
func machineWord() string {
	if runtime.GOOS == "darwin" {
		return "Mac"
	}
	return "machine"
}

// menuItems is the menu, grouped by the job someone has rather than by the
// shape of the code. The first group is the reason most people open it: moving
// to a new machine without losing anything. Each line says what it does, and
// the ones that write say so.
func menuItems() []tui.MenuItem {
	m := machineWord()
	items := []tui.MenuItem{
		{Heading: true, Label: "Moving to a new " + m},
		{Label: "1. Check nothing would be lost", Value: "ready", Hint: "unpushed work, .env files, backup age — read-only"},
		{Label: "2. Pack everything into one encrypted file", Value: "pack", Hint: "configs, keys, tokens, app list, settings"},
		{Label: "3. Restore a backup onto this " + m, Value: "restore", Hint: "pick a backup, preview, then write"},
		{Label: "4. Reinstall my apps & packages", Value: "reinstall", Hint: "from a backup's list — Homebrew, npm, …"},
		{Label: "5. What's still missing here?", Value: "doctor", Hint: "a backup's app list vs this " + m + " — read-only"},

		{Heading: true, Label: "Your private GitHub repo"},
		{Label: "Save this " + m + " to GitHub", Value: "github push", Hint: "private repo, encrypted by default"},
		{Label: "Restore from GitHub", Value: "github pull", Hint: "pick a machine, preview, then write"},
		{Label: "GitHub sign-in & status", Value: "github status", Hint: "who, which repo, which machines"},

		{Heading: true, Label: "Everyday"},
		{Label: "Quick backup to this " + m, Value: "backup", Hint: "a folder here; secrets redacted"},
		{Label: "What changed since my last backup?", Value: "status", Hint: "read-only"},
		{Label: "Choose what else to back up", Value: "include", Hint: "files & folders dothaven doesn't know"},
		{Label: "Scan my config for secrets", Value: "scan", Hint: "tokens and keys in plain files — read-only"},
		{Label: "Are my config files valid?", Value: "check", Hint: "parses each one — read-only"},
		{Label: "See everything installed", Value: "collect", Hint: "apps, packages, runtimes, fonts"},
		{Label: "Open the dashboard in your browser", Value: "ui", Hint: "coverage, backups, secrets, risks — local, read-only"},
	}
	if runtime.GOOS == "darwin" {
		items = append(items,
			tui.MenuItem{Label: "Put macOS settings back from a backup", Value: "defaults import", Hint: "CHANGES system settings — asks first"})
	}
	items = append(items,
		tui.MenuItem{Heading: true, Label: "Sync through a chezmoi repo (optional)"},
		tui.MenuItem{Label: "Check chezmoi + age setup", Value: "init", Hint: "read-only"},
		tui.MenuItem{Label: "Export configs to chezmoi", Value: "chezmoi-export", Hint: "preview first; secrets encrypted"},
		tui.MenuItem{Label: "Apply my chezmoi repo here", Value: "migrate", Hint: "WRITES to ~ and runs your install script"},

		tui.MenuItem{Heading: true, Label: ""},
		tui.MenuItem{Label: "Not sure? Answer a few questions", Value: "guide", Hint: "get the exact steps for your case"},
		tui.MenuItem{Label: "Quit", Value: "quit"},
	)
	return items
}

// actionTitles name each menu choice in the output it produces, so two
// actions run in a row don't read as one undifferentiated wall.
var actionTitles = map[string]string{
	"ready":           "Would anything be lost?",
	"pack":            "Pack everything for a new machine",
	"restore":         "Restore a backup",
	"reinstall":       "Reinstall apps & packages",
	"doctor":          "What's still missing here?",
	"backup":          "Quick backup",
	"status":          "What changed since the last backup?",
	"include":         "Choose what else to back up",
	"scan":            "Scan config for secrets",
	"check":           "Are my config files valid?",
	"collect":         "Everything installed",
	"defaults import": "Put macOS settings back",
	"init":            "Check chezmoi + age setup",
	"chezmoi-export":  "Export to chezmoi",
	"migrate":         "Apply chezmoi repo",
	"guide":           "What should I do?",
	"github push":     "Save to GitHub",
	"github pull":     "Restore from GitHub",
	"github status":   "GitHub",
	"ui":              "Dashboard",
}

// newTUICmd is the interactive front door: a menu that runs an action, shows
// its output, and comes back — until Quit, Esc or Ctrl-C.
func newTUICmd(env *sys.OS) *cobra.Command {
	return &cobra.Command{
		Use:           "tui",
		Short:         "Interactive menu — pick what to do",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !tui.Interactive() {
				fmt.Fprintln(cmd.ErrOrStderr(), "the tui command needs an interactive terminal")
				return ExitError{Code: 1}
			}
			for {
				action, err := tui.Menu("dothaven", "Keep your setup when you change machines. ↑/↓ to move, enter to pick, esc to quit.", menuItems())
				if err != nil {
					return err
				}
				if action == "" || action == "quit" {
					return nil
				}
				if rerr := runTUIAction(cmd, env, action); rerr != nil {
					// An ExitError already conveyed its outcome (e.g. a drift exit
					// code); other errors are shown. Either way, stay in the menu.
					var ee ExitError
					if !errors.As(rerr, &ee) && !errors.Is(rerr, tui.ErrAborted) {
						fmt.Fprintln(cmd.ErrOrStderr(), danger("✗ "+rerr.Error()))
					}
				}
				// A signal cancelled the shared context (Ctrl-C during an action).
				// Leave the menu rather than re-dispatch with a dead context, which
				// would make every subsequent action fail instantly.
				if cmd.Context().Err() != nil {
					return nil
				}
				pause()
			}
		},
	}
}

// pause holds the output on screen until the reader is done with it. Without
// it, the menu redraws straight over the result of the action just run.
func pause() {
	fmt.Print(dim("\n  press enter for the menu "))
	buf := make([]byte, 1)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 || buf[0] == '\n' || buf[0] == '\r' {
			break
		}
	}
	fmt.Println()
}

// runTUIAction runs one menu choice. Most dispatch to the command of the same
// name with its defaults, so the menu and the CLI behave the same; the flows
// that need more than one command are written out here.
func runTUIAction(cmd *cobra.Command, env *sys.OS, action string) error {
	title := actionTitles[action]
	if title == "" {
		title = action
	}
	printHeader(title)
	ctx := cmd.Context()

	switch action {
	case "pack":
		return runPack(cmd, env)
	case "restore":
		p, err := pickBackup(env, "Which backup should be restored?")
		if err != nil || p == "" {
			return err
		}
		return runRestore(cmd, env, p, restoreOpts{})
	case "reinstall":
		p, err := pickBackup(env, "Reinstall from which backup?")
		if err != nil || p == "" {
			return err
		}
		dir, cleanup, err := openBackup(ctx, env, p)
		defer cleanup()
		if err != nil {
			return err
		}
		return runReinstall(ctx, env, dir, false)
	case "doctor", "defaults import":
		p, err := pickBackup(env, "Which backup?")
		if err != nil || p == "" {
			return err
		}
		sub, _, _ := cmd.Root().Find(strings.Fields(action))
		sub.SetContext(ctx)
		return sub.RunE(sub, []string{p})
	case "include":
		return reviewUncovered(env, true)
	case "collect":
		sub, _, _ := cmd.Root().Find([]string{"collect"})
		sub.SetContext(ctx)
		return sub.RunE(sub, nil)
	}

	sub, _, ferr := cmd.Root().Find(strings.Fields(action))
	if ferr != nil || sub == nil {
		return ferr
	}
	// A parent command (defaults, services) carries no RunE; calling it would
	// be a nil dereference rather than an error the menu could recover from.
	if sub.RunE == nil {
		return sub.Help()
	}
	sub.SetContext(ctx)
	return sub.RunE(sub, nil)
}

// runPack is the whole "before I wipe this machine" job in one pass: check for
// work that exists nowhere else, offer anything nothing covers, write one
// encrypted file somewhere it can be carried, and prove it opens.
func runPack(cmd *cobra.Command, env *sys.OS) error {
	ctx := cmd.Context()

	fmt.Println(bold("Step 1 of 4 — anything that exists only here?"))
	r := checkReady(ctx, env, nil, 5)
	if r.cancelled {
		return ExitError{Code: 130}
	}
	if r.atRisk > 0 {
		ok, err := tui.Confirm("Some work above exists only on this machine and is not in a config backup. Continue anyway?")
		if err != nil || !ok {
			fmt.Println("Stopped. Deal with the repositories above, then pack again.")
			return ignoreAbort(err)
		}
	}

	fmt.Println("\n" + bold("Step 2 of 4 — anything else to take?"))
	if err := reviewUncovered(env, false); err != nil {
		return err
	}
	if len(collectUncovered(env, loadIncludes(env))) == 0 {
		fmt.Println(dim("  Everything that looks like config is covered."))
	}

	fmt.Println("\n" + bold("Step 3 of 4 — where should the file go?"))
	dest, err := pickDestination(env)
	if err != nil || dest == "" {
		return ignoreAbort(err)
	}
	pass, err := newPassphrase()
	if err != nil {
		return err
	}

	fmt.Println("\n" + bold("Step 4 of 4 — packing"))
	out, err := runBackup(ctx, cmd, env, backupOpts{output: dest, archive: true, encrypt: true, passphrase: pass})
	if err != nil {
		return err
	}
	n, verr := backup.Verify(out.path, func() (string, error) { return pass, nil })
	printBackupOutcome(env, out)
	if verr != nil {
		fmt.Fprintf(os.Stderr, "\n%s the file did not read back cleanly: %v — do not rely on it; pack again.\n", danger("✗"), verr)
		return ExitError{Code: 1}
	}
	fmt.Printf("\n%s %s\n", good("✓"), fmt.Sprintf("Checked: the file opens with your passphrase and holds all %d entries.", n))
	return nil
}

// pickDestination offers where to write a backup that has to leave the
// machine: any mounted drive first, then the obvious local places.
func pickDestination(env *sys.OS) (string, error) {
	var choices []tui.Choice
	if runtime.GOOS == "darwin" {
		for _, e := range readDirTimeout("/Volumes", time.Second) {
			if e.Name() == "Macintosh HD" || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			p := filepath.Join("/Volumes", e.Name())
			if fi, err := os.Stat(p); err == nil && fi.IsDir() {
				choices = append(choices, tui.Choice{Label: "Drive: " + e.Name(), Value: p, Hint: "straight onto the drive — best"})
			}
		}
	}
	choices = append(choices,
		tui.Choice{Label: "Desktop", Value: filepath.Join(env.Home(), "Desktop"), Hint: "then copy it to a drive or cloud storage"},
		tui.Choice{Label: "dothaven's folder", Value: env.DataDir(), Hint: shortHome(env, env.DataDir())},
		tui.Choice{Label: "Somewhere else…", Value: "\x00type", Hint: "type a folder path"},
	)
	choice, err := tui.Ask("Where should the backup file go?", "It must end up off this machine before you wipe it.", choices)
	if err != nil || choice != "\x00type" {
		return choice, err
	}
	p, err := tui.Input("Folder to write the backup into", "")
	if err != nil || p == "" {
		return "", err
	}
	if strings.HasPrefix(p, "~/") {
		p = filepath.Join(env.Home(), p[2:])
	}
	return filepath.Abs(p)
}
