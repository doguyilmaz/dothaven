package scan

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

var severityRank = map[Severity]int{High: 3, Medium: 2, Low: 1}

// genericPatternID marks the catch-all keyword rules. When several patterns
// match the same line at equal severity, a specific detector (e.g. "GitHub
// token") is a more useful label than the generic "secret value", so topFinding
// prefers it.
var genericPatternID = map[string]bool{"generic-secret": true, "generic-api-key": true, "secret-keyword": true}

func topFinding(r Result) Finding {
	top := r.Findings[0]
	for _, f := range r.Findings[1:] {
		fr, tr := severityRank[f.Pattern.Severity], severityRank[top.Pattern.Severity]
		if fr > tr || (fr == tr && genericPatternID[top.Pattern.ID] && !genericPatternID[f.Pattern.ID]) {
			top = f
		}
	}
	return top
}

// ReportOptions controls how the report is rendered. Colour is a parameter
// rather than something this package detects, so the decision stays with the
// layer that knows where the output is going. snapshot.Format has the same
// shape.
type ReportOptions struct {
	Color bool
	// Scan words the report for `dothaven scan`, which changes nothing: it
	// says what a plaintext backup would do with each file.
	Scan bool
}

// FormatReport renders the inline sensitivity report printed after
// collect/backup, or "" when there are no findings. Only severity is
// coloured: a HIGH in a list of thirty LOWs is what the reader is looking for.
// maxMinorLines bounds the MEDIUM and LOW lines of a report.
const maxMinorLines = 15

func FormatReport(s Summary, o ReportOptions) string {
	if len(s.Results) == 0 {
		return ""
	}
	// Sorted worst-first. Unsorted, a HIGH sat between two LOWs and the reader
	// had to scan every row to find the ones that matter.
	rows := append([]Result(nil), s.Results...)
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := severityRank[topFinding(rows[i]).Pattern.Severity], severityRank[topFinding(rows[j]).Pattern.Severity]
		if a != b {
			return a > b
		}
		return rows[i].Path < rows[j].Path
	})

	var red, yellow, dim, reset string
	if o.Color {
		red, yellow, dim, reset = "\x1b[31m", "\x1b[33m", "\x1b[2m", "\x1b[0m"
	}
	severityColor := func(sev Severity) string {
		switch sev {
		case High:
			return red
		case Medium:
			return yellow
		}
		return dim
	}

	lines := []string{"\n⚠ Sensitivity report:"}
	actionLabel := func(a Action) string {
		switch a {
		case Redact:
			if o.Scan {
				return "redacted in a plaintext backup"
			}
			return "redacted"
		case Skip:
			if o.Scan {
				return "left out of a plaintext backup"
			}
			return "skipped"
		}
		return "included"
	}
	// Every HIGH finding gets its own line. MEDIUM and LOW ones in the same
	// folder, of the same kind, share one, and there are at most
	// maxMinorLines of those: a folder of third-party files can hold
	// hundreds, and a report that long is not read.
	type group struct {
		sev        Severity
		dir, label string
		action     Action
		paths      []string
	}
	var groups []*group
	byKey := map[string]*group{}
	for _, r := range rows {
		top := topFinding(r)
		sev := top.Pattern.Severity
		if sev == High {
			groups = append(groups, &group{sev: sev, label: top.Pattern.Label, action: r.Action, paths: []string{r.Path}})
			continue
		}
		key := string(sev) + "\x00" + filepath.Dir(r.Path) + "\x00" + top.Pattern.Label + "\x00" + string(r.Action)
		g := byKey[key]
		if g == nil {
			g = &group{sev: sev, dir: filepath.Dir(r.Path), label: top.Pattern.Label, action: r.Action}
			byKey[key] = g
			groups = append(groups, g)
		}
		g.paths = append(g.paths, r.Path)
	}
	minor, hidden := 0, 0
	for _, g := range groups {
		if g.sev != High {
			if minor == maxMinorLines {
				hidden += len(g.paths)
				continue
			}
			minor++
		}
		where := g.paths
		if len(g.paths) >= 3 {
			where = []string{fmt.Sprintf("%s/ (%d files)", g.dir, len(g.paths))}
		}
		for _, w := range where {
			lines = append(lines, fmt.Sprintf("  %s%-6s%s %-30s %s%s (%s)%s",
				severityColor(g.sev), g.sev, reset, w, dim, g.label, actionLabel(g.action), reset))
		}
	}
	if hidden > 0 {
		more := "dothaven scan lists each one"
		if o.Scan {
			more = "each is listed above"
		}
		lines = append(lines, fmt.Sprintf("  %s…and %d more MEDIUM or LOW %s (%s)%s", dim, hidden, plural(hidden, "finding", "findings"), more, reset))
	}
	if o.Scan {
		lines = append(lines, "", "  An encrypted backup (dothaven backup --encrypt) keeps them as they are.")
		return strings.Join(lines, "\n")
	}
	var parts []string
	if s.Redacted > 0 {
		parts = append(parts, fmt.Sprintf("%d %s redacted", s.Redacted, plural(s.Redacted, "file", "files")))
	}
	if s.Skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d left out", s.Skipped))
	}
	if len(parts) > 0 {
		lines = append(lines, "", fmt.Sprintf("  %s. An encrypted backup (--encrypt) keeps them as they are.", strings.Join(parts, ", ")))
	}
	return strings.Join(lines, "\n")
}

var actionLabel = map[Action]string{Redact: "redact", Skip: "skip (private key)", Include: "keep"}

// FormatSecurityReport renders a standalone Markdown report grouping scanned
// files by their top severity.
func FormatSecurityReport(results []Result) string {
	var withFindings []Result
	redacted, skipped := 0, 0
	for _, r := range results {
		if len(r.Findings) == 0 {
			continue
		}
		withFindings = append(withFindings, r)
		switch r.Action {
		case Redact:
			redacted++
		case Skip:
			skipped++
		}
	}

	lines := []string{
		"# Security Report",
		"",
		fmt.Sprintf("%d file(s) scanned · %d with findings · %d to redact · %d to skip.", len(results), len(withFindings), redacted, skipped),
		"",
	}
	if len(withFindings) == 0 {
		lines = append(lines, "No sensitive data found. ✅", "")
		return strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
	}

	groups := []struct {
		sev     Severity
		heading string
	}{
		{High, "## 🔴 HIGH: secrets (masked or skipped before sync)"},
		{Medium, "## 🟡 MEDIUM"},
		{Low, "## ⚪ LOW"},
	}
	for _, g := range groups {
		var group []Result
		for _, r := range withFindings {
			if topFinding(r).Pattern.Severity == g.sev {
				group = append(group, r)
			}
		}
		if len(group) == 0 {
			continue
		}
		sort.SliceStable(group, func(i, j int) bool { return group[i].Path < group[j].Path })
		lines = append(lines, g.heading)
		for _, r := range group {
			top := topFinding(r)
			lines = append(lines, fmt.Sprintf("- `%s`: %s · %s · L%d", r.Path, top.Pattern.Label, actionLabel[r.Action], top.Line))
		}
		lines = append(lines, "")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
