package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/doguyilmaz/dothaven/internal/scan"
	"github.com/doguyilmaz/dothaven/internal/sys"
	"github.com/spf13/cobra"
)

// scanTarget stats a path and scans it as a file or directory. A missing path
// is an error; a 0-byte file is still scanned as a file (stat decides, not size).
func scanTarget(target string) ([]scan.Result, error) {
	abs, _ := filepath.Abs(target)
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("path not found: %s", abs)
	}
	if info.IsDir() {
		return scan.ScanDir(abs), nil
	}
	if r := scan.ScanFile(abs); r != nil {
		return []scan.Result{*r}, nil
	}
	return nil, nil
}

var severityRank = map[scan.Severity]int{scan.High: 3, scan.Medium: 2, scan.Low: 1}

func formatDetailed(results []scan.Result) string {
	var lines []string
	for _, r := range results {
		if len(r.Findings) == 0 {
			continue
		}
		lines = append(lines, "\n"+r.Path)
		sorted := append([]scan.Finding(nil), r.Findings...)
		sort.SliceStable(sorted, func(i, j int) bool {
			return severityRank[sorted[i].Pattern.Severity] > severityRank[sorted[j].Pattern.Severity]
		})
		for _, f := range sorted {
			lines = append(lines, fmt.Sprintf("  L%d [%s] %s: %s", f.Line, f.Pattern.Severity, f.Pattern.Label, f.Match))
		}
	}
	return strings.Join(lines, "\n")
}

func newScanCmd(_ *sys.OS) *cobra.Command {
	return &cobra.Command{
		Use:   "scan [path]",
		Short: "Scan a file or directory for sensitive data (console)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			target := "."
			if len(args) > 0 {
				target = args[0]
			}
			results, err := scanTarget(target)
			if err != nil {
				return err
			}
			any := false
			for _, r := range results {
				if len(r.Findings) > 0 {
					any = true
				}
			}
			if !any {
				fmt.Println("No sensitive data found.")
				return nil
			}
			fmt.Println(formatDetailed(results))
			fmt.Println(scan.FormatReport(scan.Summarize(results)))
			return nil
		},
	}
}

func newSecurityCmd(_ *sys.OS) *cobra.Command {
	var out string
	c := &cobra.Command{
		Use:   "security [path]",
		Short: "Write a Markdown security report (default SECURITY.md)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			target := "."
			if len(args) > 0 {
				target = args[0]
			}
			results, err := scanTarget(target)
			if err != nil {
				return err
			}
			if err := os.WriteFile(out, []byte(scan.FormatSecurityReport(results)), 0o644); err != nil {
				return err
			}
			withFindings := 0
			for _, r := range results {
				if len(r.Findings) > 0 {
					withFindings++
				}
			}
			fmt.Printf("Security report written to: %s\n  %d scanned, %d with findings.\n", out, len(results), withFindings)
			return nil
		},
	}
	c.Flags().StringVarP(&out, "output", "o", "SECURITY.md", "report output path")
	return c
}
