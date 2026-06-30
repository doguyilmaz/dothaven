package scan

import (
	"regexp"
	"sort"

	"github.com/doguyilmaz/dothaven/internal/snapshot"
)

// Marker replaces redacted values.
const Marker = "[REDACTED]"

// ApplyRedactions masks every redact-action finding's matches in content. Each
// pattern's regex runs once (a global replace), so multiple same-pattern
// secrets on one line are all masked.
func ApplyRedactions(content string, r Result) string {
	if r.Action != Redact {
		return content
	}
	seen := map[string]bool{}
	out := content
	for _, f := range r.Findings {
		if f.Pattern.Action != Redact || seen[f.Pattern.ID] {
			continue
		}
		seen[f.Pattern.ID] = true
		out = f.Pattern.re.ReplaceAllString(out, Marker)
	}
	return out
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
	npmAuthRe = regexp.MustCompile(`(?im)^(.*?(?:_authToken|_auth|_password)\s*=\s*).+$`)
	// ssh_config keywords are case-insensitive, so the lowercase forms are valid
	// syntax and must redact too. ${1} preserves the user's original casing.
	sshHostRe = regexp.MustCompile(`(?i)(HostName\s+).+`)
	sshIDRe   = regexp.MustCompile(`(?i)(IdentityFile\s+).+`)
)

func RedactIPs(text string) string { return ipRe.ReplaceAllString(text, Marker) }

func RedactNpmTokens(text string) string { return npmAuthRe.ReplaceAllString(text, "${1}"+Marker) }

func RedactSSHConfig(text string) string {
	text = sshHostRe.ReplaceAllString(text, "${1}"+Marker)
	return sshIDRe.ReplaceAllString(text, "${1}"+Marker)
}
