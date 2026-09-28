package macprefs

import (
	"regexp"
	"sort"
	"strings"

	"github.com/doguyilmaz/dothaven/internal/scan"
)

// Entry is one preference worth keeping, ready to be replayed or reviewed.
type Entry struct {
	Domain string `json:"domain"`
	Key    string `json:"key"`
	Type   string `json:"type"`
	Value  string `json:"value"`
	Action string `json:"action"`
	Reason string `json:"reason,omitempty"`
}

// Counts is what a domain contributed, for the summary line.
type Counts struct {
	Apply   int `json:"apply"`
	Review  int `json:"review"`
	Skipped int `json:"skipped"`
	Secret  int `json:"secret"`
}

// Collect turns one domain's exported plist into the entries worth keeping.
// Entries come back sorted by key, so a snapshot of the same machine is
// byte-identical run to run.
func Collect(domain string, plist []byte) ([]Entry, Counts, error) {
	values, err := parse(plist, func(key string) bool { return Portable(domain, key) })
	if err != nil {
		return nil, Counts{}, err
	}

	var entries []Entry
	var counts Counts
	for key, v := range values {
		action, reason := Classify(domain, key, v)
		if action == Skip {
			counts.Skipped++
			continue
		}

		// Preference domains do hold tokens. Anything kept here is written to
		// a file that can end up in a backup, so it goes through the same
		// scanner as every other captured file rather than a second rule set.
		value := v.S
		if v.Raw != "" {
			value = v.Raw
		}
		e := Entry{Domain: domain, Key: key, Type: typeName(v), Value: value,
			Action: action.String(), Reason: reason}
		// Scanned as key=value: an opaque token under a key named apiToken
		// has no prefix to recognise; only the name gives it away.
		pair := key + "=" + value
		res := scan.ScanContent(domain+"/"+key, pair)
		if res.Action == scan.Include && credentialKey.MatchString(key) && opaque(value) {
			res.Action = scan.Redact
			res.Findings = nil // nothing to mask by pattern: the whole value goes
		}
		switch res.Action {
		case scan.Skip:
			counts.Secret++
			continue
		case scan.Redact:
			counts.Secret++
			// A redacted value cannot be written back, only looked at.
			red, ok := strings.CutPrefix(scan.ApplyRedactions(pair, res), key+"=")
			if !ok || red == value {
				red = scan.Marker
			}
			e.Value = red
			e.Action = Review.String()
			e.Reason = "value looks like a secret"
		}

		if e.Action == Apply.String() {
			counts.Apply++
		} else {
			counts.Review++
		}
		entries = append(entries, e)
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	return entries, counts, nil
}

// credentialKey matches preference names that hold a credential. Preference
// keys are camelCase (syncApiToken, licenseKey), which the scanner's
// word-bounded keyword rules are not written for.
var credentialKey = regexp.MustCompile(`(?i)(token|secret|passw(or)?d|api[-_]?key|license[-_]?key|credential|private[-_]?key)`)

// opaque is a value that could be a credential: long, and not prose.
func opaque(v string) bool {
	return len(v) >= 12 && !strings.ContainsAny(v, " \t\n") && !strings.HasPrefix(v, "<")
}

func typeName(v Value) string {
	if v.Raw != "" {
		return "plist"
	}
	switch v.Kind {
	case String:
		return "string"
	case Int:
		return "int"
	case Float:
		return "float"
	case Bool:
		return "bool"
	}
	return "composite"
}

// WriteArgs renders the command that replays an entry, or nil when there is
// nothing safe to run — a review entry, or a value `defaults write` has no flag
// for. Returning nil rather than a best-effort command is deliberate: a caller
// cannot then run something this package did not vouch for.
func WriteArgs(e Entry) []string {
	if e.Action != "" && e.Action != Apply.String() {
		return nil
	}
	var flag string
	switch e.Type {
	case "string":
		flag = "-string"
	case "int":
		flag = "-int"
	case "float":
		flag = "-float"
	case "bool":
		flag = "-bool"
	case "plist":
		// With no type flag, `defaults write` parses the value as a property
		// list, so an XML fragment is stored as the array or dict it spells.
		if !strings.HasPrefix(e.Value, "<array") && !strings.HasPrefix(e.Value, "<dict") {
			return nil
		}
		return []string{"defaults", "write", e.Domain, e.Key, e.Value}
	default:
		return nil
	}
	return []string{"defaults", "write", e.Domain, e.Key, flag, e.Value}
}
