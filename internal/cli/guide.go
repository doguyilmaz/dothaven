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
				latestBackup:     latestBackup(env.DataDir()),
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

// askProfile asks what kind of work the user does. This is the question that
// earns its place: it changes what is worth capturing, what is worth checking,
// and what this tool does not cover — none of which is on disk to detect.
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
			"Database client configs travel (.pgpass, .my.cnf, psqlrc, mongosh). The data in those databases does not — dump anything you need separately.",
			"Homebrew service configs (nginx, mysql, redis) need `dothaven services export`; a normal backup does not include them.",
			"Docker and Podman configs travel. Images and volumes do not.",
		}
	case "frontend":
		return []string{
			"Global npm/pnpm/yarn/bun packages are recorded as a list and reinstalled, not copied.",
			"Editor extensions are captured by name; VS Code and Cursor reinstall them from that list.",
			"Browser profiles and their extensions are out of scope — use the browser's own sync.",
		}
	case "mobile":
		return []string{
			"Simulator runtimes, Android AVDs and SDK packages are recorded as a list, not copied — they are gigabytes and rebuild from a name. `dothaven list mobile` shows what you had.",
			"Signing certificates and provisioning profiles live in the Keychain and are NOT captured. Export those from Xcode yourself, or re-download them — this is the one that stops a build on the new machine.",
			"CocoaPods and Gradle caches are excluded on purpose; they rebuild from your lockfiles.",
		}
	case "devops":
		return []string{
			"kubeconfig, helm repos, terraform and cloud CLI configs travel — and most hold credentials, which is the reason to use an encrypted export rather than a plain backup.",
			"SSH config travels. SSH private keys are never written into a plain backup, by design.",
			"Run `dothaven scan ~/.kube` before sharing anything: kubeconfigs commonly embed tokens.",
		}
	case "data":
		return []string{
			"Python, conda and version-manager configs travel. Virtual environments and installed packages do not — they rebuild from your requirements or environment files.",
			"Jupyter config travels; notebooks are your own files, so back those up normally.",
		}
	case "all":
		return []string{
			"Everything installed is recorded as a list and reinstalled, not copied — that is why a backup is small and a restore needs a network.",
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
	})
	if err != nil {
		return nil, err
	}

	p := &plan{reason: "A backup copies your config files as they are. It does not reinstall anything — that is what a chezmoi setup does on the other side."}
	p.add("dothaven backup", "Timestamped copy. Secrets are redacted and private keys are never written into it.")
	if profile == "backend" || profile == "all" {
		p.add("dothaven services export", "Homebrew service configs are not part of a normal backup.")
	}
	p.add("dothaven status", "Confirms what it captured, so \"I have a backup\" is something you checked.")
	if where == "portable" {
		p.note("Copy the backup folder off this machine. A backup that lives only on the machine it backs up is not a backup.")
	}
	p.notes = append(p.notes, profileNotes(profile)...)
	return p, nil
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
		secrets, serr := ask("Do credentials need to come across?", "SSH keys, cloud logins, API tokens.", []tui.Choice{
			{Label: "Yes", Value: "yes", Hint: "they must be encrypted first"},
			{Label: "No, config only", Value: "no", Hint: "I'll log in again on the new machine"},
		})
		if serr != nil {
			return nil, serr
		}
		if secrets == "yes" {
			p.reason = "Credentials are travelling, so they have to be encrypted. That is chezmoi with an age key — and the key matters more than the files."
			if !f.chezmoiInstalled || !f.ageReady {
				p.add("dothaven init", "Sets up chezmoi and age. Do it here, on the machine you still have.")
				p.warn("Finish this before you give up the old machine.", "The setup needs files that exist only here.")
			} else {
				p.add("dothaven chezmoi-export", "Preview: which files travel plain, which get encrypted.")
				p.add("dothaven chezmoi-export --apply", "Then push the repo — that is what the new machine pulls.")
			}
		} else {
			p.reason = "Without credentials there is nothing to encrypt, so a plain backup you carry across is simpler than setting up chezmoi."
			p.add("dothaven backup", "Timestamped copy of your config.")
			p.note("Copy that folder to the new machine, then run `dothaven restore <folder>` there.")
		}
		p.notes = append(p.notes, profileNotes(profile)...)
		return p, nil
	}

	p.reason = "On the new machine the source decides the command: a chezmoi repo carries everything including encrypted files, a backup folder carries files only."
	switch {
	case f.chezmoiInstalled && f.sourceReady:
		p.add("dothaven migrate --dry-run", "Shows exactly what lands in your home folder. Writes nothing.")
		p.add("dothaven migrate", "Applies it, and runs your install script.")
	case f.latestBackup != "":
		p.add("dothaven restore --dry-run "+f.latestBackup, "Lists every file it would write, and every conflict.")
		p.add("dothaven restore "+f.latestBackup, "Asks about each conflict, and keeps a pre-restore snapshot.")
	default:
		p.add("dothaven init", "Says whether chezmoi and age are ready on this machine.")
		p.warn("Nothing to restore from yet.", "Bring a chezmoi repo or a backup folder across from the old machine first.")
	}
	p.notes = append(p.notes, profileNotes(profile)...)
	return p, nil
}

// guideWipe is the path with the only irreversible mistake in it. Config can be
// rebuilt; a stash that existed on one disk cannot. So the order is fixed.
func guideWipe(f machineFacts, ask asker) (*plan, error) {
	p := &plan{reason: "Config is replaceable and code is not, so unsaved work comes first. Everything else can be redone from a backup."}
	p.add("dothaven ready", "Every repository, checked for changes, commits and stashes that exist on no remote.")

	after, err := ask("Once that's clean, what happens to the setup?", "", []tui.Choice{
		{Label: "It moves to another computer", Value: "remote", Hint: "set that one up from this"},
		{Label: "It comes back to this one", Value: "same", Hint: "reinstalling the same machine"},
	})
	if err != nil {
		return nil, err
	}
	if after == "same" {
		p.add("dothaven backup", "Timestamped copy to restore from afterwards.")
		p.warn("Copy the backup off this machine before erasing it.", "It lives on the disk you are about to wipe.")
		return p, nil
	}
	if !f.chezmoiInstalled || !f.ageReady {
		p.add("dothaven init", "chezmoi and age are how a setup reaches another machine.")
		p.warn("Set that up before wiping.", "It needs this machine, which is the one being erased.")
		return p, nil
	}
	p.add("dothaven chezmoi-export --apply", "Then push the repo. The new machine pulls it.")
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
	p.add("dothaven doctor <other-machine.json>", "Or, on this machine: what that snapshot has and this does not.")
	p.note("`compare` is snapshot vs snapshot. `doctor` is snapshot vs the machine you run it on.")
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
		p.add("dothaven list shell", "One section at a time — the name is fuzzy-matched.")
	}
	p.notes = append(p.notes, profileNotes(profile)...)
	return p, nil
}

func printPlan(p plan) {
	fmt.Println()
	fmt.Println("Here's what I'd do:")
	fmt.Println()
	n := 0
	for _, s := range p.steps {
		if s.warn {
			fmt.Printf("  ⚠  %s\n     %s\n\n", s.cmd, s.why)
			continue
		}
		n++
		fmt.Printf("  %d. %s\n     %s\n\n", n, s.cmd, s.why)
	}
	if p.reason != "" {
		fmt.Printf("Why: %s\n", p.reason)
	}
	if len(p.notes) > 0 {
		fmt.Println("\nWorth knowing:")
		for _, note := range p.notes {
			fmt.Printf("  • %s\n", note)
		}
	}
}

// runPlanStep dispatches a step's command through the root, so it runs exactly
// as if it had been typed — same confirmations, same flags.
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
