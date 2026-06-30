package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

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
		stop := startScanProgress(&scanned)
		results, err := scan.ScanDir(ctx, abs, &scanned, true)
		stop()
		return results, err
	}
	if r := scan.ScanFile(abs); r != nil {
		return []scan.Result{*r}, nil
	}
	return nil, nil
}

// startScanProgress streams a throttled file count to stderr while a directory
// scan runs, so a long scan never looks frozen. It is a no-op when stderr isn't
// a terminal (keeps piped/CI output clean). The returned func stops the ticker
// and clears the line.
func startScanProgress(scanned *int64) func() {
	if !stderrIsTTY() {
		return func() {}
	}
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		t := time.NewTicker(150 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				fmt.Fprintf(os.Stderr, "\rscanning… %d files", atomic.LoadInt64(scanned))
			}
		}
	}()
	return func() {
		close(done)
		<-finished // wait for the ticker to stop before clearing, so no late tick re-prints
		fmt.Fprint(os.Stderr, "\r\033[K")
	}
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
			results, err := scanTarget(c.Context(), target)
			if errors.Is(err, context.Canceled) {
				fmt.Fprintln(os.Stderr, "scan cancelled.")
				return ExitError{Code: 130} // aborted ≠ clean; surface 130 for scripts/CI
			}
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
