package cli

import (
	"context"
	"errors"
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
// A directory scan honors ctx (Ctrl-C aborts it) and streams progress to stderr.
func scanTarget(ctx context.Context, target string) ([]scan.Result, error) {
	abs, _ := filepath.Abs(target)
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("path not found: %s", abs)
	}
	if info.IsDir() {
		var scanned int64
		stop := startProgress("scanning", &scanned, 0)
		results, err := scan.ScanDir(ctx, abs, &scanned, true)
		stop()
		return results, err
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
	var noFail bool
	c := &cobra.Command{
		Use:   "scan [path]",
		Short: "Scan a file or directory for secrets (exits 2 if any are HIGH)",
		Long: "Scans for secrets and prints what it finds.\n\n" +
			"Exits 2 when anything HIGH turns up, so this can gate a commit hook or a CI\n" +
			"job — a scanner that always exits 0 can only ever be read by a human, and\n" +
			"the point of scanning is to catch what a human missed. Use --no-fail for a\n" +
			"report without the verdict.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			target := "."
			if len(args) > 0 {
				target = args[0]
			}
			results, err := scanTarget(c.Context(), target)
			if errors.Is(err, context.Canceled) {
				fmt.Fprintln(os.Stderr, "scan cancelled.")
				return ExitError{Code: 130} // aborted ≠ clean; surface 130 for scripts/CI
			}
			if err != nil {
				return err
			}
			any, high := false, 0
			for _, r := range results {
				if len(r.Findings) > 0 {
					any = true
				}
				for _, f := range r.Findings {
					if f.Pattern.Severity == scan.High {
						high++
					}
				}
			}
			if !any {
				fmt.Println("No sensitive data found.")
				return nil
			}
			fmt.Println(formatDetailed(results))
			fmt.Println(scan.FormatReport(scan.Summarize(results)))
			if high > 0 && !noFail {
				fmt.Fprintf(os.Stderr, "\n%d HIGH severity finding(s). Exiting 2 — pass --no-fail to ignore.\n", high)
				return ExitError{Code: 2}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&noFail, "no-fail", false, "always exit 0, even with HIGH findings")
	return c
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
			results, err := scanTarget(c.Context(), target)
			if errors.Is(err, context.Canceled) {
				fmt.Fprintln(os.Stderr, "scan cancelled.")
				return ExitError{Code: 130} // aborted ≠ clean; surface 130 for scripts/CI
			}
			if err != nil {
				return err
			}
			if err := sys.WriteFileSecure(out, scan.FormatSecurityReport(results)); err != nil {
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
