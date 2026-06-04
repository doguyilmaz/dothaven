// Package scan detects, classifies, and redacts sensitive data in files and
// snapshot sections.
package scan

import "regexp"

type Severity string

const (
	High   Severity = "HIGH"
	Medium Severity = "MEDIUM"
	Low    Severity = "LOW"
)

type Action string

const (
	Redact  Action = "redact"  // mask matched values
	Skip    Action = "skip"    // drop the whole file/section
	Include Action = "include" // keep as-is (warn only)
)

// Pattern is one detection rule.
type Pattern struct {
	ID       string
	Label    string
	Severity Severity
	Action   Action
	re       *regexp.Regexp
}

type Finding struct {
	Pattern Pattern
	Line    int
	Match   string
}

type Result struct {
	Path     string
	Findings []Finding
	Action   Action // highest-priority action among findings (skip > redact > include)
}

type Summary struct {
	Results  []Result // only those with findings
	Redacted int
	Skipped  int
	Included int
}
