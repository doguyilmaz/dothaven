package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/doguyilmaz/dothaven/internal/health"
	"github.com/doguyilmaz/dothaven/internal/registry"
	"github.com/doguyilmaz/dothaven/internal/sys"
	"github.com/spf13/cobra"
)

// newCheckCmd validates that the config files on this machine still parse.
//
// A broken config fails quietly — a shell abandons the rest of a .zshrc after a
// syntax error, an editor ignores settings it cannot read — so it is usually
// found weeks later, by which time it has been copied into every backup and
// carried to every machine. This is the cheap version of finding out.
//
// Exits 2 when something is broken, so it can gate a backup or a commit.
func newCheckCmd(env *sys.OS) *cobra.Command {
	var showAll bool
	c := &cobra.Command{
		Use:   "check",
		Short: "Are my config files still valid? — parses each one",
		Long: "Checks the config files dothaven tracks with the parser that owns each\n" +
			"format: encoding/json for JSON, zsh -n and bash -n for shell files, git\n" +
			"and ssh for their own configs.\n\n" +
			"A config is correct when the program that reads it accepts it, so asking\n" +
			"that program beats re-implementing its parser — and adds nothing to trust.\n" +
			"Formats with no parser to hand are reported as unchecked rather than\n" +
			"assumed fine. Exits 2 if anything is broken.",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			paths := trackedFiles(env)

			var done int64
			stop := startProgress("checking config", &done, len(paths))
			results := make([]health.Result, len(paths))
			sem := make(chan struct{}, runtime.NumCPU())
			var wg sync.WaitGroup
			for i, p := range paths {
				wg.Add(1)
				go func(i int, p string) {
					defer wg.Done()
					sem <- struct{}{}
					defer func() { <-sem }()
					results[i] = health.Check(ctx, health.ExecRunner, p)
				}(i, p)
			}
			wg.Wait()
			stop()

			var broken, unchecked, ok []health.Result
			for _, r := range results {
				switch r.Status {
				case health.Broken:
					broken = append(broken, r)
				case health.Unchecked:
					unchecked = append(unchecked, r)
				default:
					ok = append(ok, r)
				}
			}
			sort.Slice(broken, func(i, j int) bool { return broken[i].Path < broken[j].Path })

			home := env.Home()
			short := func(p string) string {
				if rel, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(rel, "..") {
					return "~/" + rel
				}
				return p
			}

			for _, r := range broken {
				fmt.Printf("  ✗ %-40s %s\n", short(r.Path), r.Detail)
			}
			if showAll {
				for _, r := range ok {
					fmt.Printf("  ✓ %-40s %s\n", short(r.Path), r.Format)
				}
				for _, r := range unchecked {
					fmt.Printf("  – %-40s %s\n", short(r.Path), r.Detail)
				}
			}

			fmt.Printf("\n%d checked, %d unchecked", len(ok)+len(broken), len(unchecked))
			if !showAll && len(unchecked) > 0 {
				fmt.Print(" (--all to list them)")
			}
			fmt.Println(".")

			if len(broken) == 0 {
				fmt.Println("✅ Every config that could be parsed, parsed.")
				return nil
			}
			fmt.Printf("❌ %s broken. Fix these before they reach another machine.\n", plural(len(broken), "file"))
			return ExitError{Code: 2}
		},
	}
	c.Flags().BoolVar(&showAll, "all", false, "list files that passed and files nothing could check")
	return c
}

// trackedFiles is every registry path that exists on this machine, so the check
// covers what a backup would carry rather than the whole home directory.
func trackedFiles(env *sys.OS) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range registry.Entries {
		p, ok := e.Paths[runtime.GOOS]
		if !ok {
			continue
		}
		p = expandHome(p, env.Home())
		if seen[p] {
			continue
		}
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

func expandHome(p, home string) string {
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}
