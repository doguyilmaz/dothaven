package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/doguyilmaz/dothaven/internal/snapshot"
	"github.com/doguyilmaz/dothaven/internal/sys"
	"github.com/spf13/cobra"
)

func newCompareCmd(env *sys.OS) *cobra.Command {
	return &cobra.Command{
		Use:   "compare [file1] [file2]",
		Short: "Diff two JSON snapshots (newest two in reports/ if omitted)",
		Args:  cobra.MaximumNArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			var files []string
			if len(args) >= 2 {
				files = args[:2]
				for _, f := range files {
					if !env.Exists(f) {
						return fmt.Errorf("file not found: %s", f)
					}
				}
			} else {
				files = newestJSON(filepath.Join(cwd(), "reports"), 2)
				if len(files) < 2 {
					fmt.Println("Need at least 2 .json reports in reports/ to compare.")
					fmt.Println("Usage: dothaven compare [file1] [file2]")
					return nil
				}
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
		Use:   "list <section>",
		Short: "Print a section (fuzzy-matched) from the most recent report",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			query := args[0]
			files := newestJSON(filepath.Join(cwd(), "reports"), 1)
			if len(files) == 0 {
				fmt.Println("No .json reports found. Run 'dothaven collect' first.")
				return nil
			}
			snap, err := parseSnapshotFile(env, files[0])
			if err != nil {
				return err
			}
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
