package scan

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

var actionPriority = map[Action]int{Skip: 3, Redact: 2, Include: 1}

// MaxFileSize bounds how much of a file scanning will read into memory. Files
// larger than this are skipped — secrets live in small config files, and an
// uncapped read is a memory-exhaustion vector on an attacker-supplied tree.
const MaxFileSize = 1 << 20 // 1 MiB

// ScanContent scans text line by line against every pattern. The result's
// Action is the highest-priority action among the findings (skip > redact >
// include); no findings → include.
func ScanContent(path, content string) Result {
	var findings []Finding
	for i, line := range strings.Split(content, "\n") {
		for _, p := range Patterns() {
			if loc := p.re.FindStringIndex(line); loc != nil {
				findings = append(findings, Finding{Pattern: p, Line: i + 1, Match: truncate(line[loc[0]:loc[1]], 40)})
			}
		}
	}
	action := Include
	for _, f := range findings {
		if actionPriority[f.Pattern.Action] > actionPriority[action] {
			action = f.Pattern.Action
		}
	}
	return Result{Path: path, Findings: findings, Action: action}
}

// ScanFile scans a file's contents. A missing/unreadable path, or one larger
// than MaxFileSize, returns nil (callers may pass any path defensively).
func ScanFile(path string) *Result {
	if info, err := os.Stat(path); err != nil || info.Size() > MaxFileSize {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	r := ScanContent(path, string(b))
	return &r
}

// ScanDir recursively scans a directory, skipping node_modules/.git subtrees
// and files larger than 1 MiB. A non-directory / unreadable path yields nil.
func ScanDir(dir string) []Result {
	var out []Result
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries, keep going
		}
		if d.IsDir() {
			if n := d.Name(); n == "node_modules" || n == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if info, err := d.Info(); err == nil && info.Size() > MaxFileSize {
			return nil
		}
		if r := ScanFile(path); r != nil {
			out = append(out, *r)
		}
		return nil
	})
	return out
}

// Summarize keeps only results with findings and tallies actions.
func Summarize(results []Result) Summary {
	s := Summary{}
	for _, r := range results {
		if len(r.Findings) == 0 {
			continue
		}
		s.Results = append(s.Results, r)
		switch r.Action {
		case Redact:
			s.Redacted++
		case Skip:
			s.Skipped++
		case Include:
			s.Included++
		}
	}
	return s
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
