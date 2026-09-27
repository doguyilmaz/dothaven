package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/doguyilmaz/dothaven/internal/gitwork"
	"github.com/doguyilmaz/dothaven/internal/sys"
	"github.com/spf13/cobra"
)

// newReadyCmd answers the only question a migration really turns on: is there
// anything on this machine that a wipe would destroy for good?
//
// Config is recoverable — a dotfile you forget can be written again, a package
// reinstalled. Uncommitted changes, unpushed commits and stashes cannot be.
// They were also the one thing this tool never looked at, which made "I backed
// everything up" a claim it could not actually support.
//
// Exits 2 when something is at risk, so this can gate a wipe script.
func newReadyCmd(env *sys.OS) *cobra.Command {
	var depth int
	var roots []string
	c := &cobra.Command{
		Use:   "ready",
		Short: "Before a wipe: is anything on this machine only here? (read-only)",
		Long: "Looks through your home folder for git repositories with uncommitted changes,\n" +
			"commits that are on no remote, stashes, and gitignored files a fresh clone\n" +
			"won't bring back (.env files, keys, terraform state). Then checks how old your\n" +
			"newest backup is.\n\n" +
			"Nothing is fetched, so it is fast and works offline — which also means it\n" +
			"judges against the remote state git last saw. Exits 2 if anything is at risk.",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			r := checkReady(cmd.Context(), env, roots, depth)
			if r.cancelled {
				return ExitError{Code: 130}
			}
			if r.atRisk > 0 || r.backupStale {
				return ExitError{Code: 2}
			}
			return nil
		},
	}
	c.Flags().IntVar(&depth, "depth", 5, "how many folders deep below each root a repository can be")
	c.Flags().StringSliceVar(&roots, "root", nil, "where to look (default: your whole home folder)")
	return c
}

type readyResult struct {
	atRisk      int
	backupStale bool
	cancelled   bool
}

// checkReady looks for work that exists only on this machine and prints what
// it found. Shared by `ready` and the menu's pack-for-a-new-machine flow.
func checkReady(ctx context.Context, env *sys.OS, roots []string, depth int) readyResult {
	if len(roots) == 0 {
		roots = []string{env.Home()}
	}
	var out readyResult
	short := func(p string) string { return shortHome(env, p) }

	fmt.Println(dim("Looking for work that only exists on this machine…"))
	found := gitwork.Find(ctx, roots, depth)
	if ctx.Err() != nil {
		out.cancelled = true
		return out
	}
	var done int64
	stop := startProgress("checking repositories", &done, len(found))
	risky := gitwork.Inspect(ctx, gitwork.GitRunner, found, &done)
	stop()
	if ctx.Err() != nil {
		out.cancelled = true
		return out
	}
	sort.Slice(risky, func(i, j int) bool { return risky[i].Path < risky[j].Path })

	// Repositories with no remote come first and separately: every commit in
	// them exists only here, and the fix is not "push" but "give it somewhere
	// to be pushed to".
	var orphans, unpushed, withIgnored []gitwork.Repo
	for _, r := range risky {
		switch {
		case !r.HasRemote:
			orphans = append(orphans, r)
		case r.Dirty > 0 || r.Unsaved > 0 || r.Stashes > 0:
			unpushed = append(unpushed, r)
		}
		if len(r.Ignored) > 0 {
			withIgnored = append(withIgnored, r)
		}
	}
	for _, r := range risky {
		if r.AtRisk() {
			out.atRisk++
		}
	}

	if len(orphans) > 0 {
		fmt.Printf("\n%s\n", bold(fmt.Sprintf("%s with no remote — these exist ONLY on this machine:", plural(len(orphans), "repository"))))
		for _, r := range orphans {
			detail := plural(r.Unsaved, "commit")
			if r.Dirty > 0 {
				detail += fmt.Sprintf(", %s uncommitted", plural(r.Dirty, "file"))
			}
			if r.Stashes > 0 {
				detail += ", " + plural(r.Stashes, "stash")
			}
			fmt.Printf("  %s %s  %s\n", danger("✗"), padTo(short(r.Path), pathCol), dim(detail))
		}
	}

	if len(unpushed) > 0 {
		fmt.Printf("\n%s\n", bold(fmt.Sprintf("%s with work not pushed anywhere:", plural(len(unpushed), "repository"))))
		for _, r := range unpushed {
			var parts []string
			if r.Dirty > 0 {
				parts = append(parts, fmt.Sprintf("%s uncommitted", plural(r.Dirty, "file")))
			}
			if r.Unsaved > 0 {
				parts = append(parts, fmt.Sprintf("%s unpushed", plural(r.Unsaved, "commit")))
			}
			if r.Stashes > 0 {
				parts = append(parts, plural(r.Stashes, "stash"))
			}
			fmt.Printf("  %s %s  %s\n", warn("⚠"), padTo(short(r.Path), pathCol), dim(strings.Join(parts, ", ")))
		}
	}

	// Listed for every repo that has them, not only otherwise-clean ones:
	// pushing fixes the rest, and not these.
	if len(withIgnored) > 0 {
		fmt.Printf("\n%s\n", bold(fmt.Sprintf("%s with gitignored files a fresh clone won't bring back:", plural(len(withIgnored), "repository"))))
		for _, r := range withIgnored {
			names := r.Ignored
			more := ""
			if len(names) > 3 {
				more = fmt.Sprintf(" +%d more", len(names)-3)
				names = names[:3]
			}
			fmt.Printf("  %s %s  %s\n", warn("⚠"), padTo(short(r.Path), pathCol), dim(strings.Join(names, ", ")+more))
		}
	}

	fmt.Println()
	fmt.Println(dim(fmt.Sprintf("%s checked under %s.", plural(len(found), "repository"), strings.Join(mapStrings(roots, short), ", "))))

	// A backup older than the machine's own config is a backup that would
	// restore a machine you no longer have.
	backupNote, stale := backupFreshness(env)
	out.backupStale = stale
	fmt.Println(backupNote)

	if out.atRisk == 0 && !stale {
		fmt.Println("\n" + good("✅ Safe to wipe — everything here exists somewhere else."))
		return out
	}
	if out.atRisk > 0 {
		fmt.Printf("\n%s\n", danger(fmt.Sprintf("❌ Not safe to wipe yet: %s hold work that exists nowhere else.", plural(out.atRisk, "repository"))))
		if len(orphans) > 0 {
			fmt.Printf("   %s no remote: add one and push, or copy the folder off this machine.\n", danger("✗"))
		}
		if len(unpushed) > 0 {
			fmt.Printf("   %s commit and push. A stash is not pushed by pushing a branch.\n", warn("⚠"))
		}
		if len(withIgnored) > 0 {
			fmt.Printf("   %s ignored files: copy them over, or carry them in your encrypted backup:\n     %s\n", warn("⚠"), kbd("dothaven include <path>"))
		}
	}
	return out
}

func mapStrings(in []string, f func(string) string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = f(s)
	}
	return out
}

// pathCol is the width the repository column is held to. Long paths are
// shortened from the middle rather than allowed to shove the detail column out
// of line, which is what made a list of twenty repositories unreadable.
const pathCol = 44

// backupFreshness reports how current the newest backup is, and whether that is
// stale enough to mention.
func backupFreshness(env *sys.OS) (string, bool) {
	var newest *foundBackup
	if found := findBackups(env); len(found) > 0 {
		newest = &found[0]
	}
	if newest == nil {
		return fmt.Sprintf("  %s No backup yet — run %s.", warn("⚠"), kbd("dothaven backup --encrypt")), true
	}
	age := time.Since(newest.Mod)
	where := fmt.Sprintf("%s, %s", newest.Kind, shortHome(env, newest.Path))
	if age > 7*24*time.Hour {
		return fmt.Sprintf("  %s Newest backup is %d days old (%s) — make a fresh one: %s.", warn("⚠"), int(age.Hours()/24), where, kbd("dothaven backup --encrypt")), true
	}
	return fmt.Sprintf("  %s Newest backup is %s old %s.", good("✓"), humanAge(age), dim("("+where+")")), false
}

func humanAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "less than a minute"
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute")
	case d < 24*time.Hour:
		return plural(int(d.Hours()), "hour")
	default:
		return plural(int(d.Hours()/24), "day")
	}
}

func plural(n int, unit string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", unit)
	}
	// -ies only after a consonant: "repository" pluralises that way, "key" and
	// "day" do not.
	if l := len(unit); l >= 2 && unit[l-1] == 'y' && !strings.ContainsRune("aeiou", rune(unit[l-2])) {
		return fmt.Sprintf("%d %sies", n, strings.TrimSuffix(unit, "y"))
	}
	// -es after a sibilant (stash, branch, box), not after every h ("path").
	if strings.HasSuffix(unit, "sh") || strings.HasSuffix(unit, "ch") || strings.HasSuffix(unit, "x") || strings.HasSuffix(unit, "s") {
		return fmt.Sprintf("%d %ses", n, unit)
	}
	return fmt.Sprintf("%d %ss", n, unit)
}
