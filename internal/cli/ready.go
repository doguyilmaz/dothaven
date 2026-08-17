package cli

import (
	"fmt"
	"os"
	"path/filepath"
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
		Short: "Is this Mac safe to wipe? — checks for work that exists nowhere else",
		Long: "Looks for uncommitted changes, commits that are on no remote, and stashes,\n" +
			"then reports whether the latest backup is current.\n\n" +
			"Repositories with no remote at all are listed separately: every commit in\n" +
			"them exists only here, and pushing is not yet possible.\n\n" +
			"Nothing is fetched, so it is fast and works offline — which also means it\n" +
			"judges against the remote state git last saw. Exits 2 if anything is at risk.",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if len(roots) == 0 {
				roots = defaultRepoRoots(env)
			}

			fmt.Println(dim("Looking for work that only exists on this Mac…"))
			found := gitwork.Find(ctx, roots, depth)
			if ctx.Err() != nil {
				return ExitError{Code: 130}
			}

			var done int64
			stop := startProgress("checking repositories", &done, len(found))
			risky := gitwork.Inspect(ctx, gitwork.GitRunner, found, &done)
			stop()

			sort.Slice(risky, func(i, j int) bool { return risky[i].Path < risky[j].Path })

			home := env.Home()
			short := func(p string) string {
				if rel, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(rel, "..") {
					return "~/" + rel
				}
				return p
			}

			// Repositories with no remote come first and separately: every
			// commit in them exists only here, and the fix is not "push" but
			// "give it somewhere to be pushed to".
			var orphans, risky2 []gitwork.Repo
			for _, r := range risky {
				switch {
				case !r.HasRemote:
					orphans = append(orphans, r)
				case r.AtRisk():
					risky2 = append(risky2, r)
				}
			}

			if len(orphans) > 0 {
				fmt.Printf("\n%s\n", bold(fmt.Sprintf("%s with no remote — these exist ONLY on this Mac:", plural(len(orphans), "repository"))))
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

			if len(risky2) > 0 {
				fmt.Printf("\n%s\n", bold(fmt.Sprintf("%s with work not pushed anywhere:", plural(len(risky2), "repository"))))
				for _, r := range risky2 {
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
			atRisk := len(orphans) + len(risky2)

			fmt.Println()
			fmt.Println(dim(fmt.Sprintf("%d repositories checked.", len(found))))

			// A backup older than the machine's own config is a backup that
			// would restore a machine you no longer have.
			backupNote, backupStale := backupFreshness(env)
			fmt.Println(backupNote)

			if atRisk == 0 && !backupStale {
				fmt.Println("\n" + good("✅ Safe to wipe — everything here exists somewhere else."))
				return nil
			}
			if atRisk > 0 {
				fmt.Printf("\n%s\n", danger(fmt.Sprintf("❌ Not safe to wipe: %s hold work that exists nowhere else.", plural(atRisk, "repository"))))
				if len(orphans) > 0 {
					fmt.Printf("   %s no remote at all — add one and push, or copy the folder off this Mac.\n", danger("✗"))
				}
				if len(risky2) > 0 {
					fmt.Printf("   %s commit and push. A stash is not pushed by pushing a branch.\n", warn("⚠"))
				}
			}
			return ExitError{Code: 2}
		},
	}
	c.Flags().IntVar(&depth, "depth", 4, "how far below each root to look for repositories")
	c.Flags().StringSliceVar(&roots, "root", nil, "where to look (default: common code directories)")
	return c
}

// defaultRepoRoots are the places developers keep code. $HOME itself is
// deliberately not one: walking it costs seconds and finds the same repos
// through a slower path.
// pathCol is the width the repository column is held to. Long paths are
// shortened from the middle rather than allowed to shove the detail column out
// of line, which is what made a list of twenty repositories unreadable.
const pathCol = 44

func defaultRepoRoots(env *sys.OS) []string {
	home := env.Home()
	var roots []string
	for _, name := range []string{
		"Developer", "Projects", "Code", "code", "src", "work", "repos", "git", "dev", "workspace",
	} {
		p := filepath.Join(home, name)
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			roots = append(roots, p)
		}
	}
	if len(roots) == 0 {
		roots = []string{home}
	}
	return roots
}

// backupFreshness reports how current the newest backup is, and whether that is
// stale enough to mention.
func backupFreshness(env *sys.OS) (string, bool) {
	latest := latestBackup(env.DataDir())
	if latest == "" {
		return fmt.Sprintf("  %s No backup on this Mac yet — run %s.", warn("⚠"), kbd("dothaven backup")), true
	}
	fi, err := os.Stat(latest)
	if err != nil {
		return fmt.Sprintf("  %s No backup on this Mac yet — run %s.", warn("⚠"), kbd("dothaven backup")), true
	}
	age := time.Since(fi.ModTime())
	if age > 7*24*time.Hour {
		return fmt.Sprintf("  %s Newest backup is %d days old — run %s.", warn("⚠"), int(age.Hours()/24), kbd("dothaven backup")), true
	}
	return fmt.Sprintf("  %s Newest backup is %s old.", good("✓"), humanAge(age)), false
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
	if strings.HasSuffix(unit, "h") {
		return fmt.Sprintf("%d %ses", n, unit)
	}
	return fmt.Sprintf("%d %ss", n, unit)
}
