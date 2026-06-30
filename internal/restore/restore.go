// Package restore builds a plan from a backup directory (classifying each file
// against the live machine) and applies it. The plan-building and classification
// logic is pure and unit-tested; only Execute mutates the filesystem.
package restore

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/doguyilmaz/dothaven/internal/registry"
	"github.com/doguyilmaz/dothaven/internal/scan"
	"github.com/doguyilmaz/dothaven/internal/sys"
)

type Status string

const (
	StatusNew      Status = "new"      // in backup, absent on machine
	StatusConflict Status = "conflict" // present on machine but differs
	StatusSame     Status = "same"     // identical
	StatusRedacted Status = "redacted" // backup holds a [REDACTED] marker — unrestorable
)

// Entry is one backed-up file mapped to its live target with a status.
type Entry struct {
	BackupPath  string // path relative to the backup dir
	TargetPath  string // absolute path on the live machine
	Category    string
	Status      Status
	Sensitivity registry.Sensitivity // drives restore file perms (owner-only for medium/high)
}

// Plan is the full set of restorable entries from one backup directory.
type Plan struct {
	Entries    []Entry
	BackupDir  string
	Categories []string
}

type mapping struct {
	target   string
	category string
	isDir    bool
	sens     registry.Sensitivity
}

func buildMap(targets []registry.BackupTarget) map[string]mapping {
	m := make(map[string]mapping, len(targets))
	for _, t := range targets {
		m[t.Dest] = mapping{target: t.Src, category: t.Category, isDir: t.IsDir, sens: t.Sensitivity}
	}
	return m
}

// dirDestsByLength returns the directory-kind dests sorted longest-first (ties
// alphabetical), so a file under overlapping dests (e.g. "editor" and
// "editor/nvim") matches the most-specific one. Computed once per plan, not per
// file.
func dirDestsByLength(m map[string]mapping) []string {
	dests := make([]string, 0, len(m))
	for dest, mp := range m {
		if mp.isDir {
			dests = append(dests, dest)
		}
	}
	sort.Slice(dests, func(i, j int) bool {
		if len(dests[i]) != len(dests[j]) {
			return len(dests[i]) > len(dests[j])
		}
		return dests[i] < dests[j]
	})
	return dests
}

// matchTarget maps a backed-up file (rel, slash-separated) to its live target:
// an exact file dest, a directory-dest prefix (most-specific first, via the
// precomputed dirDests), or a `<base>.local` sibling of a file dest. A dir-prefix
// match that would escape its target base (via ../ in a crafted backup) is
// refused. Returns ("","","") when nothing matches.
func matchTarget(rel string, m map[string]mapping, dirDests []string) (target, category string, sens registry.Sensitivity) {
	if mp, ok := m[rel]; ok && !mp.isDir {
		return mp.target, mp.category, mp.sens
	}
	for _, dest := range dirDests {
		if strings.HasPrefix(rel, dest+"/") {
			mp := m[dest]
			t := filepath.Join(mp.target, rel[len(dest)+1:])
			if !contained(mp.target, t) {
				return "", "", "" // backup path escapes its destination tree (../)
			}
			return t, mp.category, mp.sens
		}
	}
	if base, ok := strings.CutSuffix(rel, ".local"); ok {
		if mp, ok := m[base]; ok && !mp.isDir {
			return mp.target + ".local", mp.category, mp.sens
		}
	}
	return "", "", ""
}

// readLiveTarget reads a live target for comparison. It reports exists=false
// when absent, and reads content only for a regular file: a symlink/FIFO/device
// is reported as existing-but-unread (os.ReadFile would follow a link or block
// forever on a pipe), and Execute refuses to write over a non-regular target.
func readLiveTarget(path string) (content string, exists bool) {
	fi, err := os.Lstat(path)
	if err != nil {
		return "", false
	}
	if !fi.Mode().IsRegular() {
		return "", true
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", true
	}
	return string(b), true
}

// contained reports whether target is base itself or lies within it (after
// cleaning) — the guard against a backup entry writing outside its tree.
func contained(base, target string) bool {
	base, target = filepath.Clean(base), filepath.Clean(target)
	return target == base || strings.HasPrefix(target, base+string(filepath.Separator))
}

// classify decides a file's restore status from its backup content and the live
// target. A [REDACTED] marker makes it unrestorable.
func classify(backupContent string, targetExists bool, targetContent string) Status {
	if strings.Contains(backupContent, scan.Marker) {
		return StatusRedacted
	}
	if !targetExists {
		return StatusNew
	}
	if backupContent == targetContent {
		return StatusSame
	}
	return StatusConflict
}

// BuildPlan walks backupDir and classifies each file against the live machine.
// A missing/empty backup dir yields an empty plan (no error).
func BuildPlan(backupDir, home string, targets []registry.BackupTarget) (Plan, error) {
	m := buildMap(targets)
	dirDests := dirDestsByLength(m)
	var entries []Entry
	catSet := map[string]bool{}

	walkErr := filepath.WalkDir(backupDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil // skip a symlink/FIFO/device in the backup tree — reading it can hang
		}
		rel, _ := filepath.Rel(backupDir, path)
		rel = filepath.ToSlash(rel)
		target, category, sens := matchTarget(rel, m, dirDests)
		if target == "" {
			return nil
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		tContent, exists := readLiveTarget(target)
		status := classify(string(raw), exists, tContent)
		catSet[category] = true
		entries = append(entries, Entry{BackupPath: rel, TargetPath: target, Category: category, Status: status, Sensitivity: sens})
		return nil
	})
	if walkErr != nil {
		return Plan{BackupDir: backupDir}, nil
	}

	cats := make([]string, 0, len(catSet))
	for c := range catSet {
		cats = append(cats, c)
	}
	sort.Strings(cats)
	return Plan{Entries: entries, BackupDir: backupDir, Categories: cats}, nil
}

// Filter narrows a plan's entries by category (skip wins; non-empty only restricts).
func Filter(p Plan, only, skip []string) Plan {
	if len(only) == 0 && len(skip) == 0 {
		return p
	}
	var kept []Entry
	catSet := map[string]bool{}
	for _, e := range p.Entries {
		if !registry.Selected(e.Category, only, skip) {
			continue
		}
		kept = append(kept, e)
		catSet[e.Category] = true
	}
	cats := make([]string, 0, len(catSet))
	for c := range catSet {
		cats = append(cats, c)
	}
	sort.Strings(cats)
	return Plan{Entries: kept, BackupDir: p.BackupDir, Categories: cats}
}

// Counts tallies entries by status.
type Counts struct{ New, Conflict, Same, Redacted int }

func Tally(entries []Entry) Counts {
	var c Counts
	for _, e := range entries {
		switch e.Status {
		case StatusNew:
			c.New++
		case StatusConflict:
			c.Conflict++
		case StatusSame:
			c.Same++
		case StatusRedacted:
			c.Redacted++
		}
	}
	return c
}

// ConflictAction is a per-file decision when a backup differs from the live file.
type ConflictAction int

const (
	ActionSkip         ConflictAction = iota // leave the live file
	ActionOverwrite                          // write the backup over it
	ActionOverwriteAll                       // and every remaining conflict
	ActionSkipAll                            // skip every remaining conflict
)

// ExecuteOptions controls how a plan is applied.
type ExecuteOptions struct {
	Force       bool   // overwrite all conflicts (default: skip them)
	SnapshotDir string // where to copy conflicts before overwriting ("" = none)
	// Resolve, when set, is asked per conflict (interactive mode). It receives
	// the entry plus the backup and live contents. nil → non-interactive: skip
	// conflicts unless Force.
	Resolve func(e Entry, backupContent, liveContent string) ConflictAction
}

// ExecuteResult summarizes an applied restore.
type ExecuteResult struct {
	Restored       int
	Skipped        int
	SkippedSymlink int    // live target was a symlink — refused, surface for manual resolution
	SnapshotDir    string // set if a pre-restore snapshot was written
	PerCategory    map[string]int
}

// Execute applies the plan to the filesystem. New files are always written;
// same/redacted entries never are. A conflict is overwritten when Force is set,
// when Resolve approves it, or once the user chose "overwrite all"; otherwise it
// is skipped. Any overwritten file is snapshotted (owner-only) first.
func Execute(plan Plan, opts ExecuteOptions) (ExecuteResult, error) {
	res := ExecuteResult{PerCategory: map[string]int{}}
	overwriteAll, skipAll := opts.Force, false

	for _, e := range plan.Entries {
		if e.Status == StatusSame || e.Status == StatusRedacted {
			res.Skipped++
			continue
		}
		// Refuse to write over a non-regular live target. A symlink would modify
		// whatever it points at (and replacing it breaks the user's link); a
		// FIFO/device/socket would block the write. Skip and surface for manual
		// resolution. (A regular file or an absent target proceeds normally.)
		if fi, err := os.Lstat(e.TargetPath); err == nil && !fi.Mode().IsRegular() {
			res.SkippedSymlink++
			continue
		}
		if e.Status == StatusConflict {
			overwrite := overwriteAll
			if !overwrite && !skipAll && opts.Resolve != nil {
				backup, _ := os.ReadFile(filepath.Join(plan.BackupDir, e.BackupPath))
				live, _ := os.ReadFile(e.TargetPath)
				switch opts.Resolve(e, string(backup), string(live)) {
				case ActionOverwrite:
					overwrite = true
				case ActionOverwriteAll:
					overwrite, overwriteAll = true, true
				case ActionSkipAll:
					skipAll = true
				}
			}
			if !overwrite {
				res.Skipped++
				continue
			}
			if opts.SnapshotDir != "" {
				if raw, err := os.ReadFile(e.TargetPath); err == nil {
					// Capture the live (unredacted) file before overwrite, owner-only.
					if err := sys.WriteFileSecure(filepath.Join(opts.SnapshotDir, e.BackupPath), string(raw)); err != nil {
						return res, err
					}
					res.SnapshotDir = opts.SnapshotDir
				}
			}
		}
		raw, err := os.ReadFile(filepath.Join(plan.BackupDir, e.BackupPath))
		if err != nil {
			return res, err
		}
		if err := writeTarget(e.TargetPath, string(raw), e.Sensitivity); err != nil {
			return res, err
		}
		res.Restored++
		res.PerCategory[e.Category]++
	}
	return res, nil
}

// writeTarget writes a restored file owner-only when the registry marked it
// medium/high, so a secret never lands world-readable; low configs keep 0644.
func writeTarget(path, content string, sens registry.Sensitivity) error {
	if sens == registry.High || sens == registry.Medium {
		return sys.WriteFileSecure(path, content)
	}
	return sys.WriteFile(path, content)
}
