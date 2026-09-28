package cli

import (
	"fmt"
	"strings"

	"github.com/doguyilmaz/dothaven/internal/snapshot"
	"github.com/doguyilmaz/dothaven/internal/sys"
	"github.com/spf13/cobra"
)

func newCompareCmd(env *sys.OS) *cobra.Command {
	return &cobra.Command{
		Use:   "compare [file1] [file2]",
		Short: "Snapshot vs snapshot: what changed between two",
		Args:  cobra.MaximumNArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			var files []string
			timeline := false
			if len(args) >= 2 {
				files = args[:2]
				for _, f := range files {
					if !env.Exists(f) {
						return fmt.Errorf("file not found: %s", f)
					}
				}
			} else {
				files = newestSnapshots(env, 2)
				if len(files) < 2 {
					fmt.Printf("Need two snapshots to compare; found %d in %s.\n", len(files), shortHome(env, snapshotDir(env)))
					fmt.Println("Run `dothaven collect` on each machine, or: dothaven compare <a.json> <b.json>")
					return nil
				}
				// Oldest on the left, so a change reads old → new; the
				// timeline format marks what the newer one added with +.
				files[0], files[1] = files[1], files[0]
				timeline = true
			}
			left, err := parseSnapshotFile(env, files[0])
			if err != nil {
				return err
			}
			right, err := parseSnapshotFile(env, files[1])
			if err != nil {
				return err
			}
			out := snapshot.Compare(left, right).Format(snapshot.FormatOptions{
				LeftLabel:   label(files[0]),
				RightLabel:  label(files[1]),
				Color:       stdoutIsTTY(),
				ChangesOnly: true,
				Timeline:    timeline,
			})
			if strings.TrimSpace(out) == "" {
				fmt.Println("No differences found.")
				return nil
			}
			fmt.Println(out)
			return nil
		},
	}
}

func fuzzyMatch(query, section string) bool {
	q, s := strings.ToLower(query), strings.ToLower(section)
	if strings.Contains(s, q) {
		return true
	}
	for _, part := range strings.Split(s, ".") {
		if strings.Contains(part, q) {
			return true
		}
	}
	return false
}

// formatSection renders one section in a human-readable form for `list`.
func formatSection(name string, s snapshot.Section) string {
	lines := []string{"[" + name + "]"}
	for _, k := range sortedStringKeys(s.Pairs) {
		lines = append(lines, "  "+k+" = "+s.Pairs[k])
	}
	for _, it := range s.Items {
		if len(it.Columns) > 1 {
			lines = append(lines, "  "+strings.Join(it.Columns, "  "))
		} else {
			lines = append(lines, "  "+it.Raw)
		}
	}
	if s.Content != nil {
		lines = append(lines, "  ---")
		for _, l := range strings.Split(*s.Content, "\n") {
			lines = append(lines, "  "+l)
		}
	}
	return strings.Join(lines, "\n")
}

func newListCmd(env *sys.OS) *cobra.Command {
	return &cobra.Command{
		Use:   "list [section] [snapshot-or-backup]",
		Short: "Print sections of the latest snapshot (or of a backup's inventory)",
		Long: "With no section, lists the section names. A section is fuzzy-matched: `list\n" +
			"brew` shows formulae, casks and the Brewfile. Reads the newest snapshot from\n" +
			"`dothaven collect`, or the inventory inside a backup you name.",
		Args: cobra.MaximumNArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			var snap snapshot.Snapshot
			var err error
			if len(args) == 2 {
				snap, err = loadSnapshotArg(c.Context(), env, args[1])
			} else {
				files := newestSnapshots(env, 1)
				if len(files) == 0 {
					fmt.Println("No snapshot yet. Run `dothaven collect` first.")
					return nil
				}
				fmt.Println(dim("From " + shortHome(env, files[0])))
				snap, err = parseSnapshotFile(env, files[0])
			}
			if err != nil {
				return err
			}
			if len(args) == 0 {
				names := make([]string, 0, len(snap))
				for n := range snap {
					names = append(names, n)
				}
				sortStrings(names)
				for _, n := range names {
					fmt.Println("  " + n)
				}
				fmt.Printf("\n%s\n", dim(fmt.Sprintf("%d sections. Show one with `dothaven list <name>`.", len(names))))
				return nil
			}
			query := args[0]
			var matches []string
			for name := range snap {
				if fuzzyMatch(query, name) {
					matches = append(matches, name)
				}
			}
			if len(matches) == 0 {
				fmt.Printf("No sections matching %q.\n", query)
				return nil
			}
			sortStrings(matches)
			for _, name := range matches {
				fmt.Println(formatSection(name, snap[name]))
			}
			return nil
		},
	}
}
