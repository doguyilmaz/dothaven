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
	warn bool // rendered as a caution rather than an instruction
}

// plan is what the guide produces: an ordered list of steps and the reasoning
// that led to them, so the answer is checkable rather than magic.
type plan struct {
	steps  []step
	reason string
}

func (p *plan) add(cmd, why string) { p.steps = append(p.steps, step{cmd: cmd, why: why}) }
func (p *plan) warn(text, why string) {
	p.steps = append(p.steps, step{cmd: text, why: why, warn: true})
}

// newGuideCmd asks a few questions and answers with the two or three commands
// that fit, in order.
//
// This exists because the command list is a menu of eighteen verbs where
// several sound alike — backup and chezmoi-export both "save your config",
// migrate and restore both "put it back" — and picking wrong is expensive in a
// tool that writes to $HOME. Choosing between them depends on facts the user
// has (does this need to reach another Mac? do secrets travel?) and facts the
// machine has (is chezmoi installed? is age configured? is there a backup?).
//
// Only the first kind is asked. Anything detectable is detected, because a
// question whose answer is already on disk is a question that wastes the
// user's attention and can be answered wrong.
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

			ctx := cmd.Context()
			state := probeInitState(ctx, env)
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

			// Offering to run the first step is the difference between advice
			// and help. It goes through the normal command, so its own
			// confirmation and --dry-run still apply.
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

// machineFacts is what the guide could work out without asking.
type machineFacts struct {
	chezmoiInstalled bool
	sourceReady      bool
	ageReady         bool
	latestBackup     string
}

// asker presents one question and returns the chosen value. Injected so the
// decision table can be exercised without a terminal — the branching is the
// part worth testing, and it is the part a prompt makes untestable.
type asker func(title, desc string, choices []tui.Choice) (string, error)

// runGuide walks the question tree.
func runGuide(f machineFacts, ask asker) (*plan, error) {
	goal, err := ask("What brings you here?", "Pick the closest one.", []tui.Choice{
		{Label: "I'm about to wipe or replace this Mac", Value: "wipe", Hint: "check nothing is lost, then save it"},
		{Label: "Save this Mac's setup", Value: "save", Hint: "so you can get it back, or move it"},
		{Label: "Set up this Mac from a saved one", Value: "setup", Hint: "new or wiped machine"},
		{Label: "See what changed", Value: "inspect", Hint: "compare against a backup or another Mac"},
		{Label: "Check for secrets I might be leaking", Value: "audit", Hint: "keys, tokens, credentials"},
	})
	if err != nil {
		return nil, err
	}

	switch goal {
	case "wipe":
		return guideWipe(f, ask)
	case "save":
		return guideSave(f, ask)
	case "setup":
		return guideSetup(f, ask)
	case "inspect":
		return guideInspect(f, ask)
	case "audit":
		return guideAudit(ask)
	}
	return nil, nil
}

// guideWipe is the path with the only irreversible mistake in it. Config can be
// rebuilt; a stash that existed on one disk cannot. So the order is fixed:
// find unsaved work first, and only then talk about backups.
func guideWipe(f machineFacts, ask asker) (*plan, error) {
	p := &plan{reason: "Config is replaceable and code is not, so unsaved work comes first. `ready` checks every repository for changes, commits and stashes that exist on no remote."}
	p.add("dothaven ready", "Lists anything a wipe would destroy. Exits 2 while something is at risk.")

	where, err := ask("Once that's clean, where should the setup go?", "", []tui.Choice{
		{Label: "Onto the new Mac", Value: "remote", Hint: "you'll set that one up from this"},
		{Label: "Just keep a copy", Value: "local", Hint: "insurance, same machine or an external disk"},
	})
	if err != nil {
		return nil, err
	}
	if where == "local" {
		p.add("dothaven backup", "Timestamped copy in your data directory. Copy that folder off the Mac too.")
		return p, nil
	}
	if !f.chezmoiInstalled || !f.ageReady {
		p.add("dothaven init", "Checks chezmoi and age, which is how config reaches another Mac.")
		p.warn("Set that up before wiping.", "It needs the old Mac, which is the one you're about to erase.")
		return p, nil
	}
	p.add("dothaven chezmoi-export", "Preview what travels, and which files get encrypted.")
	p.add("dothaven chezmoi-export --apply", "Then push your chezmoi repo — that's what the new Mac pulls.")
	return p, nil
}

func guideSave(f machineFacts, ask asker) (*plan, error) {
	where, err := ask("Where does this copy need to end up?", "", []tui.Choice{
		{Label: "Just this Mac", Value: "local", Hint: "a copy you can restore from later"},
		{Label: "Another Mac too", Value: "remote", Hint: "move or sync your setup"},
	})
	if err != nil {
		return nil, err
	}

	p := &plan{}
	if where == "local" {
		p.reason = "It stays on this Mac, so chezmoi's syncing and encryption would be setup you don't need."
		p.add("dothaven backup", "Timestamped copy. Secrets are redacted, and private keys are never written.")
		p.add("dothaven status", "Confirms it captured what you expected.")
		return p, nil
	}

	secrets, err := ask("Do private things need to travel with it?", "SSH keys, cloud credentials, API tokens.", []tui.Choice{
		{Label: "Yes", Value: "yes", Hint: "they must be encrypted first"},
		{Label: "No, config only", Value: "no", Hint: "dotfiles, editor settings, package lists"},
	})
	if err != nil {
		return nil, err
	}

	if secrets == "no" {
		p.reason = "Going to another Mac means chezmoi. Without secrets there's nothing to encrypt, so the setup is short."
		if !f.chezmoiInstalled {
			p.add("brew install chezmoi", "chezmoi stores and applies the files; dothaven decides what goes in.")
		}
		p.add("dothaven chezmoi-export", "Shows what would be added. Nothing is written yet.")
		p.add("dothaven chezmoi-export --apply", "Adds them for real.")
		return p, nil
	}

	// Secrets travel, so encryption is not optional.
	p.reason = "Secrets are travelling, so they have to be encrypted — that's chezmoi with an age key, and the key matters more than the backup."
	if !f.chezmoiInstalled || !f.ageReady {
		p.add("dothaven init", "Checks chezmoi and your age key, and tells you exactly what's missing.")
		p.warn("Set up age encryption before exporting.", "Without it, secrets would be written in the clear.")
		return p, nil
	}

	backedUp, err := ask("Is your age key backed up somewhere other than this Mac?", "A password manager, or a printed copy.", []tui.Choice{
		{Label: "Yes", Value: "yes", Hint: ""},
		{Label: "No / not sure", Value: "no", Hint: "worth fixing before you encrypt anything"},
	})
	if err != nil {
		return nil, err
	}
	if backedUp == "no" {
		p.reason = "Everything encrypted is recoverable only with that key. Losing it loses the lot, and no backup of the encrypted files helps."
		p.warn("Back up your age key first.", "Copy ~/.config/chezmoi/key.txt into a password manager.")
		p.add("dothaven chezmoi-export", "Then come back and preview what would be exported.")
		return p, nil
	}

	p.add("dothaven chezmoi-export", "Shows which files go plain and which get encrypted. Writes nothing.")
	p.add("dothaven chezmoi-export --apply", "Adds them, encrypting the sensitive ones.")
	return p, nil
}

func guideSetup(f machineFacts, ask asker) (*plan, error) {
	from, err := ask("What are you restoring from?", "", []tui.Choice{
		{Label: "A chezmoi repo", Value: "chezmoi", Hint: "you exported from another Mac"},
		{Label: "A dothaven backup folder", Value: "backup", Hint: "a local copy from `dothaven backup`"},
		{Label: "Not sure", Value: "unsure", Hint: "help me work it out"},
	})
	if err != nil {
		return nil, err
	}

	p := &plan{}
	switch from {
	case "chezmoi":
		p.reason = "A chezmoi repo carries everything including the encrypted files, so this is the full path."
		if !f.chezmoiInstalled {
			p.add("brew install chezmoi", "Needed before anything can be applied.")
		}
		if !f.sourceReady {
			p.add("chezmoi init <your-private-repo>", "Points chezmoi at your repo.")
		}
		p.add("dothaven migrate --dry-run", "Shows exactly what would land in your home folder. Writes nothing.")
		p.add("dothaven migrate", "Applies it, and runs your install script.")
		return p, nil

	case "backup":
		if f.latestBackup == "" {
			p.reason = "There's no backup on this Mac, so there's nothing here to restore from."
			p.warn("No backup found on this machine.", "Run `dothaven backup` on the Mac you want to copy from, then bring the folder here.")
			return p, nil
		}
		p.reason = "A backup restores files as they were. It won't reinstall packages — that's what a chezmoi install script does."
		p.add("dothaven restore --dry-run "+f.latestBackup, "Lists every file it would write, and every conflict.")
		p.add("dothaven restore "+f.latestBackup, "Asks about each conflict, and keeps a pre-restore snapshot.")
		return p, nil

	default:
		p.reason = "Whichever exists is the one to use — check in this order."
		p.add("dothaven init", "Says whether chezmoi and age are set up on this Mac.")
		p.add("dothaven backups", "Lists any local backups you could restore from.")
		p.warn("If neither has anything, the setup is still on your old Mac.", "Run `dothaven backup` or `dothaven chezmoi-export` there first.")
		return p, nil
	}
}

func guideInspect(f machineFacts, ask asker) (*plan, error) {
	against, err := ask("Compare this Mac against what?", "", []tui.Choice{
		{Label: "My last backup", Value: "backup", Hint: "what have I changed since?"},
		{Label: "A snapshot from another Mac", Value: "snapshot", Hint: "what's missing here?"},
		{Label: "Two snapshots", Value: "two", Hint: "what changed between them?"},
	})
	if err != nil {
		return nil, err
	}

	p := &plan{}
	switch against {
	case "backup":
		if f.latestBackup == "" {
			p.reason = "There's nothing to compare against yet."
			p.warn("No backup found.", "Run `dothaven backup` first — then this becomes useful.")
			return p, nil
		}
		p.reason = "Both read the same backup; status is the summary and diff is the detail."
		p.add("dothaven status", "One screen: how many files differ, and how stale the backup is.")
		p.add("dothaven diff", "The same comparison file by file.")
	case "snapshot":
		p.reason = "A snapshot is an inventory, so this answers 'what isn't installed here' rather than 'which files differ'."
		p.add("dothaven doctor", "Uses the newest snapshot it can find, or pass a path.")
	default:
		p.reason = "Two inventories, so this is about what appeared and disappeared between them."
		p.add("dothaven compare", "Defaults to the newest two snapshots.")
	}
	return p, nil
}

func guideAudit(ask asker) (*plan, error) {
	where, err := ask("What should I look at?", "", []tui.Choice{
		{Label: "My home folder", Value: "home", Hint: "the whole machine's config"},
		{Label: "This folder", Value: "here", Hint: "a project you're about to push"},
	})
	if err != nil {
		return nil, err
	}
	form, err := ask("How do you want the result?", "", []tui.Choice{
		{Label: "On screen", Value: "console", Hint: "read it now"},
		{Label: "A file I can keep", Value: "file", Hint: "Markdown report"},
	})
	if err != nil {
		return nil, err
	}

	target := "~"
	if where == "here" {
		target = "."
	}
	p := &plan{reason: "Scanning reads files and writes nothing, so this is always safe to run."}
	if form == "file" {
		p.add("dothaven security "+target, "Writes SECURITY.md with everything it found.")
	} else {
		p.add("dothaven scan "+target, "Prints findings by severity. Exits 2 if anything is HIGH, so a hook can use it.")
	}
	if where == "here" {
		p.add("dothaven scan . && git commit", "How to make the check block a commit.")
	}
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
}

// runPlanStep dispatches a step's command through the root, so the step runs
// exactly as if it had been typed — same confirmations, same flags.
func runPlanStep(cmd *cobra.Command, env *sys.OS, line string) error {
	fields := strings.Fields(line)
	if len(fields) < 2 || fields[0] != "dothaven" {
		// A shell instruction (brew install, chezmoi init) is for the user to
		// run; offering to execute it would reach past what this tool owns.
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
