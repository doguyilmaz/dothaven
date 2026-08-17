package scan

import (
	"fmt"
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

// FormatReport renders the inline console summary printed after collect/backup.
// Empty when there are no findings.
// ReportOptions controls how the report is rendered. Colour is a parameter
// rather than something this package detects, so the decision stays with the
// layer that knows where the output is going — the same shape snapshot.Format
// already uses.
type ReportOptions struct{ Color bool }

// FormatReport renders the sensitivity report. Severity is the one thing worth
// colouring here: it is why the report exists, and a HIGH in a list of thirty
// LOWs is what the reader is scanning for.
func FormatReport(s Summary, o ReportOptions) string {
	if len(s.Results) == 0 {
		return ""
	}
	// Sorted worst-first. Unsorted, a HIGH sat between two LOWs and the reader
	// had to scan every row to find the ones that matter; the whole point of a
	// severity is that it orders your attention.
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
	for _, r := range rows {
		top := topFinding(r)
		label := "included"
		switch r.Action {
		case Redact:
			label = "redacted"
		case Skip:
			label = "skipped"
		}
		sev := top.Pattern.Severity
		lines = append(lines, fmt.Sprintf("  %s%-6s%s %-30s %s%s — %s%s",
			severityColor(sev), sev, reset, r.Path, dim, top.Pattern.Label, label, reset))
	}
	var parts []string
	if s.Redacted > 0 {
		parts = append(parts, fmt.Sprintf("%d items redacted", s.Redacted))
	}
	if s.Skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped", s.Skipped))
	}
	if len(parts) > 0 {
		lines = append(lines, "", fmt.Sprintf("  %s. Use --no-redact to include all.", strings.Join(parts, ", ")))
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
		{High, "## 🔴 HIGH — secrets (masked or skipped before sync)"},
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
			lines = append(lines, fmt.Sprintf("- `%s` — %s · %s · L%d", r.Path, top.Pattern.Label, actionLabel[r.Action], top.Line))
		}
		lines = append(lines, "")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
}
