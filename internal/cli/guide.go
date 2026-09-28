package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/doguyilmaz/dothaven/internal/sys"
	"github.com/doguyilmaz/dothaven/internal/tui"
	"github.com/spf13/cobra"
)

// step is one instruction in a plan: the command to run and why.
type step struct {
	cmd  string
	why  string
	warn bool // a caution rather than an instruction
}

// plan is what the guide produces: ordered steps, the reasoning behind them,
// and anything specific to the kind of work the user does.
type plan struct {
	steps  []step
	reason string
	notes  []string
}

func (p *plan) add(cmd, why string) { p.steps = append(p.steps, step{cmd: cmd, why: why}) }
func (p *plan) warn(text, why string) {
	p.steps = append(p.steps, step{cmd: text, why: why, warn: true})
}
func (p *plan) note(text string) { p.notes = append(p.notes, text) }

// newGuideCmd asks what you are trying to do and answers with the commands for
// it, in order.
//
// It asks about intent and about the kind of work you do, because those change
// the answer and only you know them. It does not ask whether chezmoi is
// installed or whether a backup exists: those are on disk, and a question whose
// answer is already known wastes attention and can be answered wrong.
func newGuideCmd(env *sys.OS) *cobra.Command {
	return &cobra.Command{
		Use:           "guide",
		Short:         "Answer a few questions, get the exact commands to run",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !tui.Interactive() {
				fmt.Fprintln(cmd.ErrOrStderr(), "guide needs an interactive terminal. Try `dothaven --help`.")
				return ExitError{Code: 1}
			}

			state := probeInitState(cmd.Context(), env)
			facts := machineFacts{
				chezmoiInstalled: state.ChezmoiInstalled,
				sourceReady:      state.SourceInitialized,
				ageReady:         state.AgeKeyConfigured,
				latestBackup:     newestBackup(env),
			}

			p, err := runGuide(facts, tui.Ask)
			if err != nil {
				if err == tui.ErrAborted {
					return nil
				}
				return err
			}
			if p == nil {
				return nil
			}
			printPlan(*p)

			if len(p.steps) > 0 && !p.steps[0].warn {
				ok, cerr := tui.Confirm(fmt.Sprintf("Run step 1 now?  (%s)", p.steps[0].cmd))
				if cerr != nil || !ok {
					return nil
				}
				return runPlanStep(cmd, env, p.steps[0].cmd)
			}
			return nil
		},
	}
}

// machineFacts is what the guide worked out without asking.
type machineFacts struct {
	chezmoiInstalled bool
	sourceReady      bool
	ageReady         bool
	latestBackup     string
}

// asker presents one question and returns the chosen value. Injected so the
// decision table can be exercised without a terminal.
type asker func(title, desc string, choices []tui.Choice) (string, error)

func runGuide(f machineFacts, ask asker) (*plan, error) {
	goal, err := ask("What do you want to do?", "", []tui.Choice{
		{Label: "Back up this computer", Value: "backup", Hint: "keep a copy of how it is set up"},
		{Label: "Set up a new computer from this one", Value: "clone", Hint: "carry this setup across"},
		{Label: "Reinstall or replace this computer", Value: "wipe", Hint: "check nothing is lost first"},
		{Label: "Check my setup is healthy", Value: "health", Hint: "broken config, leaked secrets, unsaved work"},
		{Label: "Compare two computers", Value: "compare", Hint: "what one has that the other doesn't"},
		{Label: "Put my config in a private repo", Value: "repo", Hint: "version it, sync it between machines"},
		{Label: "See everything I have", Value: "see", Hint: "an inventory of this machine"},
	})
	if err != nil {
		return nil, err
	}

	switch goal {
	case "backup":
		return guideBackup(ask)
	case "clone":
		return guideClone(f, ask)
	case "wipe":
		return guideWipe(f, ask)
	case "health":
		return guideHealth(ask)
	case "compare":
		return guideCompare()
	case "repo":
		return guideRepo(f, ask)
	case "see":
		return guideSee(ask)
	}
	return nil, nil
}

// askProfile asks what kind of work the user does. The answer changes what is
// worth capturing, what is worth checking, and what this tool does not cover,
// and none of that can be detected on disk.
func askProfile(ask asker) (string, error) {
	return ask("What kind of work do you do?", "So the advice covers the right things.", []tui.Choice{
		{Label: "Backend", Value: "backend", Hint: "services, databases, containers"},
		{Label: "Frontend / web", Value: "frontend", Hint: "node, bundlers, browsers"},
		{Label: "Mobile", Value: "mobile", Hint: "iOS, Android, simulators"},
		{Label: "DevOps / infra", Value: "devops", Hint: "kubernetes, terraform, cloud CLIs"},
		{Label: "Data / ML", Value: "data", Hint: "python, notebooks, environments"},
		{Label: "A bit of everything", Value: "all", Hint: ""},
	})
}

// profileNotes is what each kind of work should know that the general advice
// does not say: what travels, and what deliberately does not, so nobody
// discovers the gap on the new machine.
func profileNotes(profile string) []string {
	switch profile {
	case "backend":
		return []string{
			"Database client configs travel (.pgpass, .my.cnf, psqlrc, mongosh). The data in those databases does not, so dump anything you need separately.",
			"Homebrew service configs (nginx, mysql, redis) need `dothaven services export`; a normal backup does not include them.",
			"Docker and Podman configs travel. Images and volumes do not.",
		}
	case "frontend":
		return []string{
			"Global npm/pnpm/yarn/bun packages are recorded as a list and reinstalled, not copied.",
			"Editor extensions are captured by name; VS Code and Cursor reinstall them from that list.",
			"Browser profiles and their extensions are out of scope. Use the browser's own sync.",
		}
	case "mobile":
		return []string{
			"Simulator runtimes, Android AVDs and SDK packages are recorded as a list, not copied, because they take gigabytes and reinstall by name. `dothaven list mobile` shows what you had.",
			"Signing certificates and provisioning profiles live in the Keychain and are NOT captured. Export them from Xcode yourself, or download them again. Without them, builds fail on the new machine.",
			"CocoaPods and Gradle caches are excluded on purpose; they rebuild from your lockfiles.",
		}
	case "devops":
		return []string{
			"kubeconfig, helm repos, terraform and cloud CLI configs travel. Most hold credentials, so use an encrypted export rather than a plain backup.",
			"SSH config travels. SSH private keys are never written into a plain backup, by design.",
			"Run `dothaven scan ~/.kube` before sharing anything: kubeconfigs commonly embed tokens.",
		}
	case "data":
		return []string{
			"Python, conda and version-manager configs travel. Virtual environments and installed packages do not. They are rebuilt from your requirements or environment files.",
			"Jupyter config travels; notebooks are your own files, so back those up normally.",
		}
	case "all":
		return []string{
			"Everything installed is recorded as a list and reinstalled, not copied. That is why a backup is small and a restore needs a network connection.",
		}
	}
	return nil
}

func guideBackup(ask asker) (*plan, error) {
	profile, err := askProfile(ask)
	if err != nil {
		return nil, err
	}
	where, err := ask("Where should the copy live?", "", []tui.Choice{
		{Label: "On this computer", Value: "local", Hint: "quick, no setup"},
		{Label: "Somewhere I can carry it", Value: "portable", Hint: "external disk, or another machine"},
		{Label: "My private GitHub repo", Value: "github", Hint: "off this machine, restorable anywhere"},
	})
	if err != nil {
		return nil, err
	}

	p := &plan{reason: "A backup copies your config, the list of what you have installed, and your macOS settings. Restoring it puts the files back; `reinstall` brings the apps back."}
	switch where {
	case "portable":
		p.add("dothaven backup --encrypt -o /Volumes/<your-drive>", "One encrypted file with everything (SSH keys, cloud logins and tokens included), plus your installed apps and macOS settings.")
		p.note("You choose a passphrase; nothing can open the file without it. Keep it in a password manager.")
	case "github":
		p.add("dothaven github push", "Creates a private repo on your account and pushes this machine to it, encrypted by default.")
	default:
		p.add("dothaven backup", "A folder here with your config, app list and macOS settings. Secrets are redacted and private keys are left out of this plaintext copy.")
	}
	if profile == "backend" || profile == "all" {
		p.add("dothaven services export", "Homebrew service configs (nginx, mysql, redis) are not part of a normal backup.")
	}
	p.add("dothaven include --list", "Lists what looks like config but is in no backup yet. Add anything you want kept.")
	if where == "local" {
		p.add("dothaven status", "Shows what the backup captured, so you can check it.")
	}
	p.notes = append(p.notes, profileNotes(profile)...)
	return p, nil
}

// askCarry asks how the setup gets from here to the other machine. The answer
// decides the commands.
func askCarry(ask asker) (string, error) {
	return ask("How do you want to carry it across?", "", []tui.Choice{
		{Label: "One encrypted file", Value: "file", Hint: "USB drive, AirDrop, cloud storage (simplest, keys included)"},
		{Label: "My private GitHub repo", Value: "github", Hint: "restore anywhere with a login and your passphrase"},
		{Label: "A chezmoi repo, kept in sync", Value: "chezmoi", Hint: "for keeping several machines in step"},
	})
}

// carrySteps are the old-machine steps for each way of carrying the setup.
func carrySteps(p *plan, f machineFacts, carry string) {
	switch carry {
	case "github":
		p.reason = "The backup goes to a private repository on your GitHub account, encrypted, so the new machine needs only a login and your passphrase."
		p.add("dothaven github push", "Signs in if needed, creates the private repo, and pushes this machine encrypted, keys and logins included.")
	case "chezmoi":
		p.reason = "chezmoi keeps machines in step from a git repo, with secrets age-encrypted. It is more setup than a file, and the age key matters more than the files."
		if !f.chezmoiInstalled || !f.ageReady {
			p.add("dothaven init", "Sets up chezmoi and age. Do it here, on the machine you still have.")
			p.warn("Finish this before you give up the old machine.", "The setup needs files that exist only here.")
			return
		}
		p.add("dothaven chezmoi-export", "Preview: which files travel plain, which get encrypted.")
		p.add("dothaven chezmoi-export --apply", "Then push the repo. The new machine pulls from it.")
	default:
		p.reason = "One encrypted file carries everything (config, keys, logins, your app list and macOS settings) and opens with your passphrase on the other machine. There is nothing else to set up."
		p.add("dothaven backup --encrypt -o /Volumes/<your-drive>", "Writes the file straight onto the drive. It is never on disk unencrypted, not even briefly.")
		p.warn("Check the file is off this machine before you wipe it.", "A backup on the disk being erased is lost with it.")
	}
}

func guideClone(f machineFacts, ask asker) (*plan, error) {
	profile, err := askProfile(ask)
	if err != nil {
		return nil, err
	}
	which, err := ask("Which computer are you on right now?", "", []tui.Choice{
		{Label: "The old one", Value: "old", Hint: "the setup I want to copy"},
		{Label: "The new one", Value: "new", Hint: "the one to set up"},
	})
	if err != nil {
		return nil, err
	}

	p := &plan{}
	if which == "old" {
		carry, err := askCarry(ask)
		if err != nil {
			return nil, err
		}
		p.add("dothaven ready", "First: uncommitted work, unpushed commits, stashes and .env files that exist only here.")
		carrySteps(p, f, carry)
		p.notes = append(p.notes, profileNotes(profile)...)
		return p, nil
	}

	p.reason = "On the new machine the source decides the command. A backup file or folder, or your GitHub repo, restores files and then reinstalls your apps; a chezmoi repo applies itself."
	switch {
	case f.chezmoiInstalled && f.sourceReady:
		p.add("dothaven migrate --dry-run", "Shows exactly what lands in your home folder. Writes nothing.")
		p.add("dothaven migrate", "Applies it, and runs your install script.")
		p.add("dothaven defaults import <backup>", "chezmoi carries files; this carries the Mac's own settings.")
	case f.latestBackup != "":
		p.add("dothaven restore --dry-run "+f.latestBackup, "Lists every file it would write, and every conflict.")
		p.add("dothaven restore "+f.latestBackup, "Lets you pick what to apply, asks about each conflict, and keeps a pre-restore snapshot.")
		p.add("dothaven defaults import "+f.latestBackup, "Puts back the Mac's own settings, which no dotfile holds.")
		p.add("dothaven reinstall "+f.latestBackup, "Installs the apps and packages you had that this machine lacks.")
	default:
		p.add("dothaven restore", "Finds backups on your drives, Downloads and Desktop and lets you pick one, or type a path.")
		p.add("dothaven restore github", "Or, if you pushed to GitHub: sign in and restore from your private repo.")
		p.warn("Nothing found on this machine yet.", "Plug in the drive with the backup file, or use the GitHub option.")
	}
	p.add("dothaven missing <backup>", "Afterwards: what the old machine had installed that this one still doesn't.")
	p.notes = append(p.notes, profileNotes(profile)...)
	return p, nil
}

// guideWipe is the path with the only irreversible mistake in it: config can be
// rebuilt, but a stash that existed on one disk cannot. Unsaved work is always
// checked first.
func guideWipe(f machineFacts, ask asker) (*plan, error) {
	p := &plan{reason: "Config is replaceable and code is not, so unsaved work comes first. Everything else can be redone from a backup."}
	p.add("dothaven ready", "Every repository, checked for changes, commits, stashes and ignored .env files that exist on no remote.")

	after, err := ask("Once that's clean, what happens to the setup?", "", []tui.Choice{
		{Label: "It moves to another computer", Value: "remote", Hint: "set that one up from this"},
		{Label: "It comes back to this one", Value: "same", Hint: "reinstalling the same machine"},
	})
	if err != nil {
		return nil, err
	}
	if after == "same" {
		p.add("dothaven backup --encrypt -o /Volumes/<your-drive>", "Everything, keys included, in one encrypted file to restore from afterwards.")
		p.warn("Copy the backup off this machine before erasing it.", "It lives on the disk you are about to wipe unless you wrote it to a drive.")
		return p, nil
	}
	carry, err := askCarry(ask)
	if err != nil {
		return nil, err
	}
	reason := p.reason
	carrySteps(p, f, carry)
	p.reason = reason + " " + p.reason
	return p, nil
}

func guideHealth(ask asker) (*plan, error) {
	profile, err := askProfile(ask)
	if err != nil {
		return nil, err
	}
	p := &plan{reason: "Three different kinds of unhealthy: config that no longer parses, secrets sitting in plain files, and work that exists on no remote."}
	p.add("dothaven check", "Parses your config files and reports the ones that are broken.")
	p.add("dothaven scan ~", "Finds keys, tokens and credentials in plain files. Exits 2 if anything is HIGH.")
	p.add("dothaven ready", "Finds uncommitted, unpushed and stashed work.")
	p.notes = append(p.notes, profileNotes(profile)...)
	return p, nil
}

func guideCompare() (*plan, error) {
	p := &plan{reason: "Comparing machines means comparing inventories, so both sides need a snapshot. A backup is files, and files do not tell you what is installed."}
	p.add("dothaven collect", "Run this on BOTH machines. Each writes a timestamped JSON snapshot.")
	p.add("dothaven compare a.json b.json", "What one has that the other does not.")
	p.add("dothaven missing <other-machine.json>", "Or, on this machine: what that snapshot has and this does not.")
	p.note("`compare` is snapshot vs snapshot. `missing` is snapshot vs the machine you run it on.")
	return p, nil
}

func guideRepo(f machineFacts, ask asker) (*plan, error) {
	secrets, err := ask("Will it hold credentials?", "SSH keys, cloud logins, tokens.", []tui.Choice{
		{Label: "Yes", Value: "yes", Hint: "then it must be encrypted, private repo or not"},
		{Label: "No, config only", Value: "no", Hint: ""},
	})
	if err != nil {
		return nil, err
	}

	p := &plan{reason: "chezmoi is the repo: it stores the files, encrypts what needs it, and applies them elsewhere. dothaven decides what goes in."}
	if !f.chezmoiInstalled {
		p.add("brew install chezmoi", "The storage layer.")
	}
	if !f.sourceReady {
		p.add("chezmoi init", "Creates the source repo at ~/.local/share/chezmoi.")
	}
	if secrets == "yes" {
		p.add("dothaven init", "Walks through the age key that encrypts the sensitive files.")
		if !f.ageReady {
			p.warn("Do not add secrets before age is configured.", "They would be committed in plain text, and git remembers.")
		}
	}
	p.add("dothaven chezmoi-export", "Preview what would be added, plain vs encrypted.")
	p.add("dothaven chezmoi-export --apply", "Adds them.")
	p.note("Make the remote private, not public: even encrypted files reveal which services you use.")
	return p, nil
}

func guideSee(ask asker) (*plan, error) {
	profile, err := askProfile(ask)
	if err != nil {
		return nil, err
	}
	p := &plan{reason: "A snapshot is the inventory; `list` reads one section of it without the noise of the rest."}
	p.add("dothaven collect", "Writes a timestamped JSON snapshot of everything found.")
	switch profile {
	case "backend":
		p.add("dothaven list db", "Just the database section. Try `devops` and `cloud` too.")
	case "frontend":
		p.add("dothaven list editor", "Editors and extensions. Try `lang` and `npm` too.")
	case "mobile":
		p.add("dothaven list lang", "Runtimes and SDK versions. Try `vm` for version managers.")
	case "devops":
		p.add("dothaven list cloud", "Cloud CLI configs. Try `devops` and `ssh` too.")
	case "data":
		p.add("dothaven list lang", "Python and friends. Try `vm` for environments.")
	default:
		p.add("dothaven list shell", "One section at a time. The name is fuzzy-matched.")
	}
	p.notes = append(p.notes, profileNotes(profile)...)
	return p, nil
}

func printPlan(p plan) {
	fmt.Println()
	fmt.Println(bold("What to do:"))
	fmt.Println()
	n := 0
	plain := func(s string) string { return s }
	for _, s := range p.steps {
		if s.warn {
			fmt.Printf("  %s  %s\n     %s\n\n", warn("⚠"), warn(s.cmd), paragraph(s.why, "     ", dim))
			continue
		}
		n++
		fmt.Printf("  %s %s\n     %s\n\n", dim(fmt.Sprintf("%d.", n)), kbd(s.cmd), paragraph(s.why, "     ", dim))
	}
	if p.reason != "" {
		fmt.Printf("%s %s\n", bold("Why:"), paragraph(p.reason, "     ", plain))
	}
	if len(p.notes) > 0 {
		fmt.Println("\n" + bold("Worth knowing:"))
		for _, note := range p.notes {
			fmt.Printf("  %s %s\n", dim("•"), paragraph(note, "    ", plain))
		}
	}
}

// runPlanStep dispatches a step's command through the root, so it runs as if
// typed, with the same confirmations and flags.
func runPlanStep(cmd *cobra.Command, env *sys.OS, line string) error {
	fields := strings.Fields(line)
	if len(fields) < 2 || fields[0] != "dothaven" {
		// A shell instruction (brew install, chezmoi init) is for the user to
		// run; executing it would reach past what this tool owns.
		fmt.Fprintf(os.Stderr, "Run that one yourself: %s\n", line)
		return nil
	}
	sub, rest, ferr := cmd.Root().Find(fields[1:])
	if ferr != nil || sub == nil || sub.RunE == nil {
		return ferr
	}
	if err := sub.ParseFlags(rest); err != nil {
		return err
	}
	sub.SetContext(cmd.Context())
	fmt.Println()
	return sub.RunE(sub, sub.Flags().Args())
}
