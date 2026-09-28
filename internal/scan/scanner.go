package scan

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

var actionPriority = map[Action]int{Skip: 3, Redact: 2, Include: 1}

// MaxFileSize bounds how much of a file scanning will read into memory. Files
// larger than this are skipped — secrets live in small config files, and an
// uncapped read is a memory-exhaustion vector on an attacker-supplied tree.
const MaxFileSize = 1 << 20 // 1 MiB

// maxLineLen bounds the length of a single line fed to the regex set. A real
// secret sits in a short config line; a longer line is almost always minified
// code or an embedded data blob, and matching every pattern against it is wasted
// work (a 1 MiB single-line file would otherwise be scanned against every
// pattern). Such lines are skipped rather than truncated, since a truncated
// match would report a misleading column.
const maxLineLen = 64 << 10 // 64 KiB

// skipDirs are subtree names never worth scanning: VCS metadata, dependency and
// build caches, the macOS trash, and cloud-storage mounts (the latter can block
// on network I/O). Pruning them keeps a scan from drowning in machine-generated
// files. This is the single source of truth for directory-walk pruning.
var skipDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true, "CVS": true,
	"node_modules": true, "vendor": true,
	".cache": true, "Caches": true, "CloudStorage": true, ".Trash": true,
	".gradle": true, ".m2": true, "Pods": true, ".terraform": true,
	".venv": true, "venv": true, "__pycache__": true, ".npm": true,
}

// ScanContent scans text line by line against every pattern. The result's
// Action is the highest-priority action among the findings (skip > redact >
// include); no findings → include. Lines over maxLineLen are skipped, which is
// right for a walk over a whole home directory and wrong for a gate.
func ScanContent(path, content string) Result { return scanContent(path, content, maxLineLen) }

// ScanContentFull is ScanContent without the line-length shortcut. A gate —
// the check that decides whether a file may be written somewhere in plaintext
// — cannot skip the one long line a single-line JSON config keeps its token on.
func ScanContentFull(path, content string) Result { return scanContent(path, content, 0) }

func scanContent(path, content string, maxLine int) Result {
	pats := Patterns() // hoisted out of the line loop
	// Binary content is matched only against key-material rules; see looksBinary.
	binary := looksBinary(content)
	var findings []Finding
	action := Include
	var redact []Pattern
	seenRedact := map[string]bool{}
	for i, line := range strings.Split(content, "\n") {
		if maxLine > 0 && len(line) > maxLine {
			continue // minified/data line — not where secrets live, and costly to scan
		}
		lineStart := len(findings)
		var spans [][2]int // where each of this line's findings matched
		for _, p := range pats {
			if binary && p.Action != Skip {
				continue
			}
			loc := p.re.FindStringIndex(line)
			if loc == nil {
				continue
			}
			match := line[loc[0]:loc[1]]
			if !p.real(match) {
				// The first match on a line can be noise while a later one is
				// not: `127.0.0.1 … 10.2.3.4`.
				if !anyReal(p, line) {
					continue
				}
				match = firstReal(p, line)
				if j := strings.Index(line, match); j >= 0 {
					loc = []int{j, j + len(match)}
				}
			}
			// Kept whole (within reason): Preview shows its last characters,
			// and a match cut short would show "..".
			findings = append(findings, Finding{Pattern: p, Line: i + 1, Match: truncate(match, 512)})
			spans = append(spans, [2]int{loc[0], loc[1]})
			// The verdict and the redaction come from every match; only the
			// report below is deduplicated.
			if actionPriority[p.Action] > actionPriority[action] {
				action = p.Action
			}
			if p.Action == Redact && !seenRedact[p.ID] {
				seenRedact[p.ID] = true
				redact = append(redact, p)
			}
		}
		findings = dedupeLine(findings, lineStart, spans)
	}
	return Result{Path: path, Findings: findings, Action: action, redact: redact}
}

// ScanFile scans a regular file's contents. A missing/unreadable path, a
// non-regular file (symlink, device, FIFO, socket), or one larger than
// MaxFileSize returns nil — callers may pass any path defensively, and reading a
// device or FIFO would otherwise block forever.
func ScanFile(path string) *Result {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > MaxFileSize {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	r := ScanContent(path, string(b))
	return &r
}

// ScanDir recursively scans every regular file under dir, skipping symlinks/
// devices and files larger than MaxFileSize. File scans run on a worker pool
// (one per CPU) since each scan is independent. The walk aborts and returns
// ctx.Err() when ctx is cancelled. If progress is non-nil it is incremented
// (atomically) once per file scanned, letting a caller report progress without
// blocking the walk. When prune is true, dependency/cache/VCS subtrees
// (skipDirs) are skipped — right for a user-facing scan, but a security probe
// that must not miss a secret (chezmoi export) passes false to scan everything.
func ScanDir(ctx context.Context, dir string, progress *int64, prune bool) ([]Result, error) {
	paths := make(chan string)
	var walkErr error
	go func() {
		defer close(paths)
		walkErr = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if cerr := ctx.Err(); cerr != nil {
				return cerr // abort the walk on cancellation
			}
			if err != nil {
				return nil // skip unreadable entries, keep going
			}
			if d.IsDir() {
				if prune && skipDirs[d.Name()] {
					return fs.SkipDir
				}
				return nil
			}
			// Every non-dir entry (symlinks included) goes to a worker. ScanFile
			// resolves the target with os.Stat and skips non-regular files, so a
			// symlink-to-device/FIFO can't block the read while a symlink-to-a-
			// regular-file is still scanned (os.Stat doesn't block on a device).
			select {
			case paths <- path:
			case <-ctx.Done():
				return ctx.Err()
			}
			return nil
		})
	}()

	var (
		mu  sync.Mutex
		out []Result
		wg  sync.WaitGroup
	)
	for i := 0; i < runtime.NumCPU(); i++ {
		wg.Go(func() {
			defer func() {
				// A scan of one file must never crash the whole process; recover and
				// drop just that file (RE2 is panic-safe, so this is belt-and-braces,
				// mirroring RunCollectors' isolation).
				if rec := recover(); rec != nil {
					fmt.Fprintf(os.Stderr, "dothaven: scan worker recovered: %v\n", rec)
				}
			}()
			for path := range paths {
				r := ScanFile(path)
				if progress != nil {
					atomic.AddInt64(progress, 1)
				}
				if r != nil {
					mu.Lock()
					out = append(out, *r)
					mu.Unlock()
				}
			}
		})
	}
	wg.Wait()

	// Workers append in completion order; sort by path for stable output.
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })

	if errors.Is(walkErr, context.Canceled) || errors.Is(walkErr, context.DeadlineExceeded) {
		return out, walkErr
	}
	return out, nil
}

func anyReal(p Pattern, line string) bool { return firstReal(p, line) != "" }

func firstReal(p Pattern, line string) string {
	for _, m := range p.re.FindAllString(line, -1) {
		if p.real(m) {
			return m
		}
	}
	return ""
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

// dedupeLine drops a keyword finding (`TOKEN=…`) that overlaps another on
// the same line — a specific one (the ghp_… it assigns) or an earlier keyword
// rule: one secret, reported once, by its most telling name.
func dedupeLine(findings []Finding, start int, spans [][2]int) []Finding {
	line := findings[start:]
	if len(line) < 2 {
		return findings
	}
	keep := findings[:start]
	for i, f := range line {
		dup := false
		if f.Pattern.keyword {
			for j, g := range line {
				overlap := spans[i][0] < spans[j][1] && spans[j][0] < spans[i][1]
				// A specific rule wins over a keyword one; of two keyword
				// rules, the first listed. Never a weaker one: an email
				// address inside a password does not make it an email.
				if j != i && overlap && (!g.Pattern.keyword || j < i) &&
					actionPriority[g.Pattern.Action] >= actionPriority[f.Pattern.Action] {
					dup = true
					break
				}
			}
		}
		if !dup {
			keep = append(keep, f)
		}
	}
	return keep
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
