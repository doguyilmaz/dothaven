package scan

import (
	"regexp"
	"sort"
	"strings"

	"github.com/doguyilmaz/dothaven/internal/snapshot"
)

// Marker replaces redacted values.
const Marker = "[REDACTED]"

// ApplyRedactions masks every redact-action finding's matches in content, one
// line at a time, the way the scan found them. A match can therefore never
// run from one line into the next — which is how `token =` on one line could
// otherwise swallow the key of `secret = x` on the next and leave x behind.
// Every match on a line is masked, not just the first.
func ApplyRedactions(content string, r Result) string {
	if r.Action != Redact {
		return content
	}
	seen := map[string]bool{}
	var rules []Pattern
	for _, f := range r.Findings {
		if f.Pattern.Action != Redact || seen[f.Pattern.ID] {
			continue
		}
		seen[f.Pattern.ID] = true
		rules = append(rules, f.Pattern)
	}
	var b strings.Builder
	b.Grow(len(content))
	for line := range strings.SplitAfterSeq(content, "\n") {
		body, nl := strings.CutSuffix(line, "\n")
		for _, p := range rules {
			body = p.re.ReplaceAllStringFunc(body, func(m string) string {
				// Only what the scan would itself report is masked. A keyword
				// rule also matches `token == x` in code, and masking that
				// corrupts a file to hide nothing.
				if !p.real(m) {
					return m
				}
				if p.keyword {
					return keepKey(m)
				}
				return Marker
			})
		}
		b.WriteString(body)
		if nl {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// keepKey masks the value of a `key = value` match and keeps the key, so a
// redacted file still says which setting held the secret.
func keepKey(m string) string {
	i := strings.IndexAny(m, "=:")
	if i < 0 {
		return Marker
	}
	j := i + 1
	for j < len(m) && strings.ContainsRune(" \t=:\"'", rune(m[j])) {
		j++
	}
	return m[:j] + Marker
}

// RedactSection scrubs a section in place — content AND pairs (values and keys)
// AND items — so no section type bypasses the gate. It returns kept=false when
// the section must be dropped entirely (its content scanned to "skip", e.g. a
// private key), plus the scan results for the run's summary.
func RedactSection(name string, s *snapshot.Section) (kept bool, results []Result) {
	if s.Content != nil && *s.Content != "" {
		r := ScanContent(name, *s.Content)
		results = append(results, r)
		if r.Action == Skip {
			return false, results
		}
		if r.Action == Redact {
			red := ApplyRedactions(*s.Content, r)
			s.Content = &red
		}
	}

	for _, k := range sortedMapKeys(s.Pairs) {
		// A secret can live in the KEY itself (a flattened JSON object keyed by a
		// token). A key can't be masked in place, so drop the whole pair.
		if ks := ScanContent(name, k); ks.Action != Include {
			results = append(results, ks)
			delete(s.Pairs, k)
			continue
		}
		// Scan the reconstructed `key=value`, not the value alone: an opaque
		// secret (no recognizable prefix) under a credential-named key — e.g. a
		// flattened JSON `auth.apiKey` => <random> — only trips the keyword
		// patterns when the keyword and a delimiter sit on the same line.
		if r := ScanContent(name+"."+k, k+"="+s.Pairs[k]); r.Action != Include {
			results = append(results, r)
			s.Pairs[k] = Marker
		}
	}

	for i := range s.Items {
		if r := ScanContent(name, s.Items[i].Raw); r.Action != Include {
			results = append(results, r)
			s.Items[i].Raw = Marker
			for j := range s.Items[i].Columns {
				s.Items[i].Columns[j] = Marker
			}
		}
	}

	return true, results
}

func sortedMapKeys(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// --- Targeted, structure-preserving redactors (used by registry entries) ---

var (
	ipRe = regexp.MustCompile(`\b(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}\b`)
	// npm auth lines: _authToken=, plus the legacy _auth= / _password= (base64)
	// forms, with or without a //registry:-scoped prefix. Per line, any case.
	npmAuthRe = regexp.MustCompile(`(?im)^(.*?(?:_authToken|_auth|_password)[ \t]*=[ \t]*).+$`)
	// ssh_config keywords are case-insensitive, so the lowercase forms are valid
	// syntax and must redact too. ${1} preserves the user's original casing.
	sshHostRe = regexp.MustCompile(`(?i)(HostName[ \t]+).+`)
	sshIDRe   = regexp.MustCompile(`(?i)(IdentityFile[ \t]+).+`)
)

func RedactIPs(text string) string { return ipRe.ReplaceAllString(text, Marker) }

func RedactNpmTokens(text string) string { return npmAuthRe.ReplaceAllString(text, "${1}"+Marker) }

func RedactSSHConfig(text string) string {
	text = sshHostRe.ReplaceAllString(text, "${1}"+Marker)
	return sshIDRe.ReplaceAllString(text, "${1}"+Marker)
}

// Preview is a finding's match made safe to print: enough to recognise it (the
// setting's name, a token's prefix, its last two characters), never the value.
// A scan's output lands in terminal scrollback, CI logs and screen shares.
func Preview(match string) string {
	if strings.Contains(match, "PRIVATE KEY") || strings.HasPrefix(match, "(") {
		return match // a PEM/s-expression header names the kind, not the key
	}
	key, val := "", match
	if i := strings.IndexAny(match, "=:"); i >= 0 && i < len(match)-1 {
		key, val = match[:i+1], strings.TrimLeft(match[i+1:], " \t\"'")
	}
	r := []rune(val)
	if len(r) <= 8 {
		return key + "••••"
	}
	head := 4
	if key != "" {
		head = 0
	}
	return key + string(r[:head]) + "••••" + string(r[len(r)-2:])
}
