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
	BackupPath string // path relative to the backup dir
	TargetPath string // absolute path on the live machine
	Category   string
	Status     Status
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
}

func buildMap(targets []registry.BackupTarget) map[string]mapping {
	m := make(map[string]mapping, len(targets))
	for _, t := range targets {
		m[t.Dest] = mapping{target: t.Src, category: t.Category, isDir: t.IsDir}
	}
	return m
}

// matchTarget maps a backed-up file (rel, slash-separated) to its live target:
// an exact file dest, a directory-dest prefix, or a `<base>.local` sibling of a
// file dest. Returns ("","") when nothing matches.
func matchTarget(rel string, m map[string]mapping) (target, category string) {
	if mp, ok := m[rel]; ok && !mp.isDir {
		return mp.target, mp.category
	}
	dirDests := make([]string, 0, len(m))
	for dest, mp := range m {
		if mp.isDir {
			dirDests = append(dirDests, dest)
		}
	}
	sort.Strings(dirDests) // deterministic when dests could overlap
	for _, dest := range dirDests {
		if strings.HasPrefix(rel, dest+"/") {
			mp := m[dest]
			return filepath.Join(mp.target, rel[len(dest)+1:]), mp.category
		}
	}
	if base, ok := strings.CutSuffix(rel, ".local"); ok {
		if mp, ok := m[base]; ok && !mp.isDir {
			return mp.target + ".local", mp.category
		}
	}
	return "", ""
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
	var entries []Entry
	catSet := map[string]bool{}

	walkErr := filepath.WalkDir(backupDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(backupDir, path)
		rel = filepath.ToSlash(rel)
		target, category := matchTarget(rel, m)
		if target == "" {
			return nil
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		tRaw, tErr := os.ReadFile(target)
		status := classify(string(raw), tErr == nil, string(tRaw))
		catSet[category] = true
		entries = append(entries, Entry{BackupPath: rel, TargetPath: target, Category: category, Status: status})
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
	contains := func(list []string, s string) bool {
		for _, x := range list {
			if x == s {
				return true
			}
		}
		return false
	}
	var kept []Entry
	catSet := map[string]bool{}
	for _, e := range p.Entries {
		if contains(skip, e.Category) {
			continue
		}
		if len(only) > 0 && !contains(only, e.Category) {
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
	Restored    int
	Skipped     int
	SnapshotDir string // set if a pre-restore snapshot was written
	PerCategory map[string]int
}

// Execute applies the plan to the filesystem. New files are always written;
// same/redacted entries never are. A conflict is overwritten when Force is set,
// when Resolve approves it, or once the user chose "overwrite all"; otherwise it
// is skipped. Any overwritten file is snapshotted (owner-only) first.
func Execute(plan Plan, opts ExecuteOptions) (ExecuteResult, error) {
	res := ExecuteResult{PerCategory: map[string]int{}}
	overwriteAll, skipAll := opts.Force, false

	for _, e := range plan.Entries {
		switch e.Status {
		case StatusSame, StatusRedacted:
			res.Skipped++
			continue
		case StatusConflict:
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
		if err := sys.WriteFile(e.TargetPath, string(raw)); err != nil {
			return res, err
		}
		res.Restored++
		res.PerCategory[e.Category]++
	}
	return res, nil
}
