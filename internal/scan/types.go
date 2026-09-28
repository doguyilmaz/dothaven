// Package scan detects, classifies, and redacts sensitive data in files and
// snapshot sections.
package scan

import (
	"regexp"
	"strings"
)

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
	// need lists text every match contains (lowercase when fold is set, for a
	// case-insensitive rule). A file or line with none of it cannot match, so
	// the regex is not run: most config files hold no secret, and running
	// every rule over every line made a large readable backup take minutes.
	// pre is the same test for a rule no single piece of text captures.
	need []string
	fold bool
	pre  func(s string) bool
}

// possible reports whether s could hold a match. lower is s lowercased, for
// case-insensitive rules.
func (p *Pattern) possible(s, lower string) bool {
	if p.pre != nil {
		return p.pre(s)
	}
	if len(p.need) == 0 {
		return true
	}
	hay := s
	if p.fold {
		hay = lower
	}
	for _, n := range p.need {
		if strings.Contains(hay, n) {
			return true
		}
	}
	return false
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
	Findings []Finding // what is reported: one per secret, by its most specific rule
	Action   Action    // highest-priority action among all matches (skip > redact > include)
	// redact are the redact rules that matched anywhere, before findings were
	// deduplicated for the report. Redaction uses these, never the report:
	// a rule dropped from the report as a duplicate on one line can still be
	// the only one that matches a secret elsewhere.
	redact []Pattern
}

type Summary struct {
	Results  []Result // only those with findings
	Redacted int
	Skipped  int
	Included int
}
