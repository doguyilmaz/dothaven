package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/doguyilmaz/dothaven/internal/registry"
	"github.com/doguyilmaz/dothaven/internal/sys"
	"github.com/spf13/cobra"
)

// includePath is the user's own list of extra paths. It lives with the rest of
// their config (and is itself backed up), not in dothaven's data directory, so
// it survives a restore onto a new machine.
func includePath(env *sys.OS) string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "dothaven", "include")
	}
	return filepath.Join(env.Home(), ".config", "dothaven", "include")
}

func loadIncludes(env *sys.OS) registry.Includes {
	b, err := os.ReadFile(includePath(env))
	if err != nil {
		return registry.Includes{}
	}
	return registry.ParseIncludes(string(b), env.Home())
}

func saveIncludes(env *sys.OS, inc registry.Includes) error {
	return sys.WriteFile(includePath(env), registry.FormatIncludes(inc))
}

// allEntries is the registry plus the user's includes: everything a backup,
// restore or export on this machine covers.
func allEntries(env *sys.OS) []registry.Entry {
	isDir := func(p string) bool {
		fi, err := os.Stat(p)
		return err == nil && fi.IsDir()
	}
	entries := append([]registry.Entry(nil), registry.Entries...)
	// The include list follows XDG_CONFIG_HOME; the registry names the
	// default place. Point its entry at wherever the list is, so a
	// relocated list is still carried (a copy: the registry is shared).
	if rel, err := filepath.Rel(env.Home(), includePath(env)); err == nil && !strings.HasPrefix(rel, "..") {
		for i := range entries {
			if entries[i].ID == "dothaven.include" {
				p := "~/" + filepath.ToSlash(rel)
				entries[i].Paths = map[string]string{"darwin": p, "linux": p}
			}
		}
	}
	entries = append(entries, registry.IncludeEntries(loadIncludes(env).Paths, isDir, env.Home())...)
	for _, e := range registry.IncludeEntries(gitReferenced(env), isDir, env.Home()) {
		e.Name += " (from your git config)"
		entries = append(entries, e)
	}
	return entries
}

// gitReferenced is what this machine's git config points at in the home
// folder (hooks, the ignore file, included configs) that no registry entry
// already carries. Those files travel with the config without being added:
// they are carried like includes and restored to the same place.
func gitReferenced(env *sys.OS) []string {
	home := env.Home()
	var out []string
	seen := map[string]bool{}
	for _, cfg := range []string{filepath.Join(home, ".gitconfig"), filepath.Join(home, ".config", "git", "config")} {
		b, err := os.ReadFile(cfg)
		if err != nil {
			continue
		}
		for _, p := range registry.GitReferences(string(b), filepath.Dir(cfg), home) {
			if seen[p] || registryCovers(p) {
				continue
			}
			if _, err := os.Stat(filepath.Join(home, p[2:])); err != nil {
				continue
			}
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// registryCovers reports whether a registry entry already carries p (the
// path, or a folder holding it).
func registryCovers(p string) bool {
	for _, e := range registry.Entries {
		for _, q := range e.Paths {
			if q == p || strings.HasPrefix(p, strings.TrimSuffix(q, "/")+"/") {
				return true
			}
		}
	}
	return false
}

// uncovered lists what looks like config and nothing covers yet (declined
// paths included: the user said no, which is not the same as covered).
func uncovered(env *sys.OS) []string {
	inc := loadIncludes(env)
	inc.Declined = nil
	return collectUncovered(env, inc)
}

func newIncludeCmd(env *sys.OS) *cobra.Command {
	var remove, list, review bool
	c := &cobra.Command{
		Use:   "include [path...]",
		Short: "Add your own files and folders to every backup",
		Long: "dothaven knows a few hundred config locations. Add everything else you care\n" +
			"about here: a tool nobody else uses, a scripts folder, an app's config dir.\n\n" +
			"  dothaven include ~/.config/raycast ~/bin   add paths (kept for every backup)\n" +
			"  dothaven include --remove ~/bin            stop carrying one\n" +
			"  dothaven include --list                    what you added, and what isn't covered\n\n" +
			"Paths must be inside your home folder; restore puts them back in the same place.\n" +
			"The list lives in " + "~/.config/dothaven/include" + " and travels with your backups.",
		RunE: func(cmd *cobra.Command, args []string) error {
			inc := loadIncludes(env)
			home := env.Home()
			if len(args) == 0 && !remove && (list || !review) {
				printIncludes(env, inc)
				return nil
			}
			if review {
				_, err := reviewUncovered(env, true)
				return err
			}
			changed := 0
			for _, a := range args {
				p, ok := registry.NormalizeInclude(expandArg(a), home)
				if !ok {
					fmt.Fprintf(os.Stderr, "  %s %s is not inside your home folder (skipped)\n", warn("⚠"), a)
					continue
				}
				if remove {
					if i := slices.Index(inc.Paths, p); i >= 0 {
						inc.Paths = slices.Delete(inc.Paths, i, i+1)
						fmt.Printf("  %s %s\n", dim("−"), p)
						changed++
					}
					continue
				}
				if !env.Exists(filepath.Join(home, p[2:])) {
					fmt.Fprintf(os.Stderr, "  %s %s does not exist (added anyway; it will be picked up once it does)\n", warn("⚠"), p)
				}
				if i := slices.Index(inc.Declined, p); i >= 0 {
					inc.Declined = slices.Delete(inc.Declined, i, i+1)
				}
				if !slices.Contains(inc.Paths, p) {
					inc.Paths = append(inc.Paths, p)
					fmt.Printf("  %s %s\n", good("+"), p)
					changed++
				}
			}
			if changed == 0 {
				fmt.Println("Nothing changed.")
				return nil
			}
			if err := saveIncludes(env, inc); err != nil {
				return err
			}
			fmt.Printf("%s %s\n", dim("Saved to"), dim(includePath(env)))
			return nil
		},
	}
	c.Flags().BoolVar(&remove, "remove", false, "remove the given paths instead of adding them")
	c.Flags().BoolVar(&list, "list", false, "show what you added and what nothing covers yet")
	c.Flags().BoolVar(&review, "review", false, "pick from the untracked files and folders interactively")
	return c
}

// expandArg resolves a command-line path the way a shell user expects: "~/x"
// and relative paths are both accepted here, unlike in the include file.
func expandArg(a string) string {
	if len(a) > 1 && a[:2] == "~/" {
		return a
	}
	if abs, err := filepath.Abs(a); err == nil {
		return abs
	}
	return a
}

func printIncludes(env *sys.OS, inc registry.Includes) {
	if len(inc.Paths) == 0 {
		fmt.Println(dim("You haven't added any paths yet."))
	} else {
		fmt.Println(bold("Added by you (in every backup):"))
		for _, p := range inc.Paths {
			fmt.Printf("  %s %s\n", good("+"), p)
		}
	}
	if u := uncovered(env); len(u) > 0 {
		fmt.Printf("\n%s\n", bold(fmt.Sprintf("Not covered by anything (%d), so not in your backups:", len(u))))
		for _, p := range u {
			fmt.Printf("  %s %s\n", warn("?"), p)
		}
		fmt.Printf("\nAdd with %s, or pick interactively with %s.\n", kbd("dothaven include <path>"), kbd("dothaven include --review"))
	}
	if len(inc.Declined) > 0 {
		fmt.Printf("\n%s\n", dim(fmt.Sprintf("%d path(s) you chose to leave out (lines starting with ! in %s).", len(inc.Declined), includePath(env))))
	}
}
