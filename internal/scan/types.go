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
	// keyword rules fire on a word plus a delimiter, so they also match code
	// that merely mentions the word — `token ==` in a shell parser is not a
	// secret. Their matches are checked by valueLooksReal before reporting.
	// Structural rules (a PEM header, an AWS key's shape) need no such check.
	keyword bool
	// check, when set, vets a match before it is reported — for rules whose
	// shape also fits things that are not secret (127.0.0.1 is an IP address
	// and tells nobody anything).
	check func(match string) bool
}

// real reports whether a match survives the rule's own vetting.
func (p Pattern) real(match string) bool {
	if p.keyword && !valueLooksReal(match) {
		return false
	}
	return p.check == nil || p.check(match)
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
