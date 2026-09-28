package cli

import (
	"context"
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

// menuTree is the menu, grouped by the job someone has rather than by the
// shape of the code. Each group is short enough to show whole on any
// terminal. The first group is the reason most people open it: moving to a
// new machine without losing anything.
func menuTree() []tui.Node {
	m := machineWord()
	move := []tui.Node{
		{Label: "Check nothing would be lost", Value: "ready", About: "Unpushed work, .env files and the age of your last backup. Read-only."},
		{Label: "Pack everything into one encrypted file", Value: "pack", About: "Config, keys, tokens, the app list and settings, in one file only your passphrase opens."},
		{Label: "Restore a backup onto this " + m, Value: "restore", About: "Pick a backup, see what it would change, then write."},
		{Label: "Reinstall my apps and packages", Value: "reinstall", About: "From a backup's app list: Homebrew, npm, pipx and the rest."},
		{Label: "What's still missing here?", Value: "missing", About: "A backup's app list compared with this " + m + ". Read-only."},
	}
	if runtime.GOOS == "darwin" {
		move = append(move, tui.Node{Label: "Put macOS settings back", Value: "defaults import", About: "Key repeat, Dock, Finder and more, from a backup. Asks before it changes anything."})
	}
	return []tui.Node{
		{Label: "Move to a new " + m, About: "Check, pack, restore and reinstall.", Children: move},
		{Label: "GitHub backup", About: "Keep your backup in a private GitHub repository.", Children: []tui.Node{
			{Label: "Save this " + m + " to GitHub", Value: "github push", About: "Private repository, encrypted by default. Signs you in if needed."},
			{Label: "Restore from GitHub", Value: "github pull", About: "Pick a machine, see what it would change, then write."},
			{Label: "Sign-in and status", Value: "github status", About: "Who you are signed in as, the repository, and the machines in it."},
			{Label: "Sign out", Value: "github logout", About: "Forget dothaven's GitHub token and remembered passphrase on this " + m + "."},
		}},
		{Label: "Everyday", About: "Quick backups and what they cover.", Children: []tui.Node{
			{Label: "Quick backup to this " + m, Value: "backup", About: "A folder in dothaven's data directory. Secrets are redacted."},
			{Label: "What changed since my last backup?", Value: "status", About: "Read-only."},
			{Label: "Choose what else to back up", Value: "include", About: "Files and folders dothaven does not know about yet."},
			{Label: "Open the dashboard", Value: "ui", About: "Coverage, backups, secrets and risks in your browser. Local and read-only."},
		}},
		{Label: "Check this " + m, About: "Secrets, broken config, what is installed.", Children: []tui.Node{
			{Label: "Scan my config for secrets", Value: "scan", About: "Tokens and keys sitting in plain files. Read-only."},
			{Label: "Are my config files valid?", Value: "check", About: "Parses each one. Read-only."},
			{Label: "See everything installed", Value: "collect", About: "Apps, packages, runtimes and fonts."},
			{Label: "Check dothaven itself", Value: "doctor", About: "Can dothaven do its job here: keychain, tools, GitHub, disk."},
		}},
		{Label: "chezmoi sync (optional)", About: "Keep your config in your own chezmoi repository.", Children: []tui.Node{
			{Label: "Check chezmoi and age setup", Value: "init", About: "Read-only."},
			{Label: "Export configs to chezmoi", Value: "chezmoi-export", About: "Shows what it would do first. Secrets are encrypted."},
			{Label: "Apply my chezmoi repo here", Value: "migrate", About: "Writes to your home folder and runs your install script."},
		}},
		{Label: "Not sure? Answer a few questions", Value: "guide", About: "Get the exact steps for your case."},
		{Label: "Quit", Value: "quit"},
	}
}

// actionTitles head the output of each menu choice, so two actions run in a
// row are easy to tell apart.
var actionTitles = map[string]string{
	"ready":           "Would anything be lost?",
	"pack":            "Pack everything for a new machine",
	"restore":         "Restore a backup",
	"reinstall":       "Reinstall apps & packages",
	"missing":         "What's still missing here?",
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
	"github logout":   "Sign out of GitHub",
	"doctor":          "Check dothaven itself",
	"ui":              "Dashboard",
}

// newTUICmd is the interactive menu: a full-screen list that runs an action
// in the normal terminal, shows its output, and opens again where it was,
// until Quit, Esc or q.
func newTUICmd(env *sys.OS) *cobra.Command {
	return &cobra.Command{
		Use:           "tui",
		Short:         "Interactive menu: pick what to do",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !tui.Interactive() {
				fmt.Fprintln(cmd.ErrOrStderr(), "the tui command needs an interactive terminal")
				return ExitError{Code: 1}
			}
			root := cmd.Context()
			var at tui.Place
			for {
				action, place, err := tui.RunMenu("dothaven", func() string { return menuStatus(env) }, menuTree(), at)
				if err != nil {
					return err
				}
				if action == "" || action == "quit" {
					return nil
				}
				at = place
				runlog.stepf("menu: %s", action)

				// Ctrl-C during an action cancels that action only, and the
				// menu comes back (see CancelAction).
				ctx, cancel := context.WithCancel(root)
				startAction(cancel)
				cmd.SetContext(ctx)
				rerr := runTUIAction(cmd, env, action)
				cancelled := ctx.Err() != nil
				cmd.SetContext(root)
				endAction()
				cancel()
				var ee ExitError
				switch {
				case action == "ui" && rerr == nil:
					// Ctrl-C is how the dashboard is stopped; it says so itself.
				case cancelled || errors.Is(rerr, context.Canceled):
					fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled.")
				case rerr == nil:
				case !errors.As(rerr, &ee) && !errors.Is(rerr, tui.ErrAborted):
					fmt.Fprintln(cmd.ErrOrStderr(), danger("✗ "+rerr.Error()))
				}
				if root.Err() != nil {
					return nil
				}
				pause(root)
				// Ctrl-C at the prompt leaves the menu.
				if root.Err() != nil {
					return nil
				}
			}
		},
	}
}

// menuStatus is the top right corner of the menu: this machine, and whether
// GitHub is signed in. Local only: it asks GitHub nothing.
func menuStatus(env *sys.OS) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	gh := "GitHub: not signed in"
	if _, src, err := resolveToken(ctx, env, false); src != "" {
		gh = "GitHub: signed in"
		if errors.Is(err, errRenewDue) {
			gh = "GitHub: sign-in due for renewal"
		}
	}
	return hostname() + " · " + gh
}

// pause holds the output on screen until the reader is done with it. Without
// it, the menu redraws straight over the result of the action just run.
func pause(ctx context.Context) {
	fmt.Print(dim("\n  Press Enter to go back to the menu "))
	waitEnter(ctx)
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
		dir, cleanup, err := openBackupOnly(ctx, env, p, "inventory")
		defer cleanup()
		if err != nil {
			return err
		}
		return runReinstall(ctx, env, dir, false)
	case "missing", "defaults import":
		p, err := pickBackup(env, "Which backup?")
		if err != nil || p == "" {
			return err
		}
		sub, _, _ := cmd.Root().Find(strings.Fields(action))
		sub.SetContext(ctx)
		return sub.RunE(sub, []string{p})
	case "include":
		_, err := reviewUncovered(env, true)
		return err
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
	if action == "scan" {
		// The exit code is for hooks and CI; in the menu it only adds a line
		// about flags nobody here passes.
		_ = sub.Flags().Set("no-fail", "true")
	}
	sub.SetContext(ctx)
	return sub.RunE(sub, nil)
}

// runPack is the whole "before I wipe this machine" job in one pass: check for
// work that exists nowhere else, offer anything nothing covers, write one
// encrypted file somewhere it can be carried, and check that it opens.
func runPack(cmd *cobra.Command, env *sys.OS) error {
	ctx := cmd.Context()

	fmt.Println(bold("Step 1 of 4: anything that exists only here?"))
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

	fmt.Println("\n" + bold("Step 2 of 4: anything else to take?"))
	if _, err := reviewUncovered(env, false); err != nil {
		return err
	}
	if len(collectUncovered(env, loadIncludes(env))) == 0 {
		fmt.Println(dim("  Everything that looks like config is covered."))
	}

	fmt.Println("\n" + bold("Step 3 of 4: where should the file go?"))
	dest, err := pickDestination(env)
	if err != nil || dest == "" {
		return ignoreAbort(err)
	}
	pass, err := newPassphrase()
	if err != nil {
		return err
	}

	fmt.Println("\n" + bold("Step 4 of 4: packing"))
	out, err := runBackup(ctx, cmd, env, backupOpts{output: dest, archive: true, encrypt: true, passphrase: pass})
	if err != nil {
		return err
	}
	n, verr := backup.Verify(out.path, func() (string, error) { return pass, nil })
	printBackupOutcome(env, out)
	if verr != nil {
		fmt.Fprintf(os.Stderr, "\n%s the file did not read back cleanly: %v. Do not rely on it; pack again.\n", danger("✗"), verr)
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
				choices = append(choices, tui.Choice{Label: "Drive: " + e.Name(), Value: p, Hint: "straight onto the drive (best)"})
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
