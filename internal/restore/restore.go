// Package restore builds a plan from a backup directory (classifying each file
// against the live machine) and applies it. The plan-building and classification
// logic is pure and unit-tested; only Execute mutates the filesystem.
package restore

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/doguyilmaz/dothaven/internal/registry"
	"github.com/doguyilmaz/dothaven/internal/scan"
	"github.com/doguyilmaz/dothaven/internal/sys"
)

type Status string

const (
	StatusNew      Status = "new"      // in backup, absent on machine
	StatusConflict Status = "conflict" // present on machine but differs
	StatusSame     Status = "same"     // identical
	StatusRedacted Status = "redacted" // backup holds a [REDACTED] marker, so it cannot be restored
	// Ledger-backed statuses (see Ledger.refine).
	StatusUpdate  Status = "update"  // live file is what restore wrote earlier; the backup is newer
	StatusChanged Status = "changed" // restore wrote it earlier, and it has been edited since
	StatusSkipped Status = "skipped" // declined on an earlier run, and the backup's copy is unchanged
)

// Actionable reports whether a status is something restore could still do.
func (s Status) Actionable() bool {
	return s != StatusSame && s != StatusRedacted
}

// Entry is one backed-up file mapped to its live target with a status.
type Entry struct {
	BackupPath  string // path relative to the backup dir
	TargetPath  string // absolute path on the live machine
	Category    string
	Status      Status
	Sensitivity registry.Sensitivity // drives restore file perms (owner-only for medium/high)
	Exec        bool                 // the backed-up file was executable (a hook, a script)
	BackupSHA   string
	LiveSHA     string    // "" when there is no live file
	AppliedAt   time.Time // when restore last wrote it, if the ledger knows
	Rewritten   bool      // the old machine's home path was rewritten to this one's
}

// Plan is the full set of restorable entries from one backup directory.
type Plan struct {
	Entries    []Entry
	BackupDir  string
	BackupID   string // the backup's own name, stable however it was carried
	Categories []string
	// Unmatched are backup files with no place on this machine: config of a
	// tool that lives elsewhere (or nowhere) on this OS, or one this version
	// does not know. They are listed, never silently dropped.
	Unmatched []string
	// Unreadable are backup files that could not be read.
	Unreadable []string
	// Rewrite, when set, is applied to every backup file before it is
	// compared or written (see HomeRewriter).
	Rewrite func([]byte) []byte
}

// backupContent reads an entry's file from the backup as it would be written.
func (p Plan) backupContent(e Entry) ([]byte, error) {
	raw, err := os.ReadFile(filepath.Join(p.BackupDir, e.BackupPath))
	if err == nil && p.Rewrite != nil {
		raw = p.Rewrite(raw)
	}
	return raw, err
}

// metaPath reports whether a backup file is the backup's own bookkeeping (its
// manifest, inventory and settings), which restore does not map to a config
// location. The next steps handle those.
func metaPath(rel string) bool {
	return rel == "MANIFEST.txt" || rel == "dothaven.json" || rel == "README.md" || rel == "secrets.tar.gz.age" ||
		strings.HasPrefix(rel, "inventory/") || strings.HasPrefix(rel, "macos-defaults/")
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

// contained reports whether target is base itself or lies within it after
// cleaning. It is the guard against a backup entry writing outside its tree.
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
	return BuildPlanWith(backupDir, home, targets, nil)
}

// BuildPlanWith is BuildPlan with the ledger's memory of earlier runs.
func BuildPlanWith(backupDir, home string, targets []registry.BackupTarget, lg *Ledger) (Plan, error) {
	return BuildPlanRewriting(backupDir, home, targets, lg, nil)
}

// HomeRewriter returns what rewrites the old machine's home folder to this
// one's in a text file: a backup made as /Users/dogu restored where home is
// /Users/dogu.yilmaz, or on Linux as /home/dogu. An absolute path into a
// home that does not exist here is never what anyone wants, whether it is a
// PATH entry, an editor setting, a hook command or
// `includeIf "gitdir:/Users/dogu/work/"`.
// Only a whole path component matches (/Users/dogu, not /Users/doguyilmaz),
// and binary files are left alone. nil when there is nothing to rewrite.
func HomeRewriter(oldHome, newHome string) func([]byte) []byte {
	old, nw := filepath.Clean(oldHome), filepath.Clean(newHome)
	if !filepath.IsAbs(old) || old == "/" || old == nw {
		return nil
	}
	re := regexp.MustCompile(regexp.QuoteMeta(old) + `($|[^A-Za-z0-9._-])`)
	oldB := []byte(old)
	return func(b []byte) []byte {
		if !bytes.Contains(b, oldB) || scan.LooksBinary(b) {
			return b
		}
		return re.ReplaceAllFunc(b, func(m []byte) []byte {
			return append([]byte(nw), m[len(old):]...)
		})
	}
}

// BuildPlanRewriting is BuildPlanWith with each backup file passed through
// rewrite (HomeRewriter) before it is compared and written, so hashes, the
// ledger and diffs all see the content that would land.
func BuildPlanRewriting(backupDir, home string, targets []registry.BackupTarget, lg *Ledger, rewrite func([]byte) []byte) (Plan, error) {
	m := buildMap(targets)
	dirDests := dirDestsByLength(m)
	backupID := filepath.Base(filepath.Clean(backupDir))
	var entries []Entry
	var unmatched, unreadable []string
	catSet := map[string]bool{}

	walkErr := filepath.WalkDir(backupDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil // skip a symlink/FIFO/device in the backup tree: reading it can hang
		}
		rel, _ := filepath.Rel(backupDir, path)
		rel = filepath.ToSlash(rel)
		target, category, sens := matchTarget(rel, m, dirDests)
		if target == "" {
			if !metaPath(rel) {
				unmatched = append(unmatched, rel)
			}
			return nil
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			unreadable = append(unreadable, rel)
			return nil
		}
		rewritten := false
		if rewrite != nil {
			if r := rewrite(raw); !bytes.Equal(r, raw) {
				raw, rewritten = r, true
			}
		}
		exec := false
		if fi, ierr := d.Info(); ierr == nil {
			exec = fi.Mode().Perm()&0o111 != 0
		}
		tContent, exists := readLiveTarget(target)
		status := classify(string(raw), exists, tContent)
		catSet[category] = true
		e := Entry{BackupPath: rel, TargetPath: target, Category: category, Status: status, Sensitivity: sens, Exec: exec, BackupSHA: Hash(raw), Rewritten: rewritten}
		if exists {
			e.LiveSHA = Hash([]byte(tContent))
		}
		lg.refine(&e, backupID)
		entries = append(entries, e)
		return nil
	})
	if walkErr != nil {
		return Plan{BackupDir: backupDir, BackupID: backupID}, walkErr
	}

	cats := make([]string, 0, len(catSet))
	for c := range catSet {
		cats = append(cats, c)
	}
	sort.Strings(cats)
	return Plan{Entries: entries, BackupDir: backupDir, BackupID: backupID, Categories: cats,
		Unmatched: unmatched, Unreadable: unreadable, Rewrite: rewrite}, nil
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
	return Plan{Entries: kept, BackupDir: p.BackupDir, BackupID: p.BackupID, Categories: cats,
		Unmatched: p.Unmatched, Unreadable: p.Unreadable, Rewrite: p.Rewrite}
}

// Counts tallies entries by status.
type Counts struct{ New, Conflict, Same, Redacted, Update, Changed, Skipped int }

func Tally(entries []Entry) Counts {
	var c Counts
	for _, e := range entries {
		c.Add(e.Status)
	}
	return c
}

// Add counts one entry of status s.
func (c *Counts) Add(s Status) {
	switch s {
	case StatusNew:
		c.New++
	case StatusConflict:
		c.Conflict++
	case StatusSame:
		c.Same++
	case StatusRedacted:
		c.Redacted++
	case StatusUpdate:
		c.Update++
	case StatusChanged:
		c.Changed++
	case StatusSkipped:
		c.Skipped++
	}
}

// ConflictAction is a per-file decision when a backup differs from the live file.
type ConflictAction int

const (
	ActionSkip         ConflictAction = iota // leave the live file
	ActionOverwrite                          // write the backup over it
	ActionOverwriteAll                       // and every remaining conflict
	ActionSkipAll                            // skip every remaining conflict
	ActionStop                               // stop here: nothing more is written
)

// ExecuteOptions controls how a plan is applied.
type ExecuteOptions struct {
	Force       bool   // overwrite all conflicts (default: skip them)
	SnapshotDir string // where to copy conflicts before overwriting ("" = none)
	// Resolve, when set, is asked per conflict (interactive mode). It receives
	// the entry plus the backup and live contents. nil → non-interactive: skip
	// conflicts unless Force.
	Resolve func(e Entry, backupContent, liveContent string) ConflictAction
	// Selected, when set, narrows the run to the entries the user picked.
	Selected func(e Entry) bool
	// DeclineUnselected records entries Selected left out as declined. Set it
	// when the user saw each file and unpicked it; leave it off when they
	// picked categories, so the rest is still on offer next time.
	DeclineUnselected bool
	// Approved, when set, marks entries the user explicitly chose to
	// overwrite (picked by name), so they are written without a second ask.
	Approved func(e Entry) bool
}

// Outcome is what happened to one entry.
type Outcome struct {
	Entry   Entry
	Written bool
	// Declined is a decision to remember: the user was shown this file and
	// said no. The next restore lists it as skipped rather than offering it.
	Declined bool
	// Kept is a file that differs, left alone because nobody was there to
	// ask (off a terminal). It is still offered next time.
	Kept bool
	// NoCopy is a file that differs and was not replaced because the current
	// version could not be read, and so could not be kept aside first.
	NoCopy bool
}

// ExecuteResult summarizes an applied restore.
type ExecuteResult struct {
	Restored       int
	Skipped        int
	SkippedSymlink int    // live target was a symlink: refused, left for the user to resolve
	SnapshotDir    string // set if a pre-restore snapshot was written
	PerCategory    map[string]int
	Outcomes       []Outcome
	// Stopped is set when Resolve said stop: the entries from there on were
	// not looked at, and nothing about them is remembered.
	Stopped bool
}

// Execute applies the plan to the filesystem. New and updated files are
// written; same/redacted entries never are. A conflict (or a file changed since
// it was applied, or one skipped before) is overwritten when Force is set, when
// it was approved by name, when Resolve approves it, or once the user chose
// "overwrite all"; otherwise it is kept. Any overwritten file is snapshotted
// (owner-only) first.
func Execute(plan Plan, opts ExecuteOptions) (ExecuteResult, error) {
	res := ExecuteResult{PerCategory: map[string]int{}}
	overwriteAll, skipAll := opts.Force, false

	for _, e := range plan.Entries {
		if !e.Status.Actionable() {
			res.Skipped++
			continue
		}
		if opts.Selected != nil && !opts.Selected(e) {
			res.Skipped++
			if opts.DeclineUnselected {
				res.Outcomes = append(res.Outcomes, Outcome{Entry: e, Declined: true})
			}
			continue
		}
		// A file declined on an earlier run stays declined unless it is picked
		// again or everything is being forced; being asked twice is the thing
		// the ledger exists to prevent.
		if e.Status == StatusSkipped && opts.Selected == nil && !opts.Force {
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
		differs := e.Status == StatusConflict || e.Status == StatusChanged ||
			(e.Status == StatusSkipped && e.LiveSHA != "")
		if differs {
			overwrite := overwriteAll || (opts.Approved != nil && opts.Approved(e))
			if !overwrite && !skipAll && opts.Resolve != nil {
				backup, _ := plan.backupContent(e)
				live, _ := os.ReadFile(e.TargetPath)
				switch opts.Resolve(e, string(backup), string(live)) {
				case ActionOverwrite:
					overwrite = true
				case ActionOverwriteAll:
					overwrite, overwriteAll = true, true
				case ActionSkipAll:
					skipAll = true
				case ActionStop:
					res.Stopped = true
					return res, nil
				}
			}
			if !overwrite {
				res.Skipped++
				// Remembered only when somebody decided: asked, or picked by
				// hand. Kept off a terminal, it stays on offer.
				decided := opts.Resolve != nil || opts.Selected != nil
				res.Outcomes = append(res.Outcomes, Outcome{Entry: e, Declined: decided, Kept: !decided})
				continue
			}
		}
		// Anything already on disk is snapshotted before it is replaced,
		// including an update of restore's own earlier write. A file that cannot
		// be read cannot be kept aside, so it is not replaced either.
		if e.LiveSHA != "" && opts.SnapshotDir != "" {
			raw, err := os.ReadFile(e.TargetPath)
			if err != nil {
				res.Skipped++
				res.Outcomes = append(res.Outcomes, Outcome{Entry: e, NoCopy: true})
				continue
			}
			// Capture the live (unredacted) file before overwrite, owner-only.
			if err := sys.WriteFileSecure(filepath.Join(opts.SnapshotDir, e.BackupPath), string(raw)); err != nil {
				return res, err
			}
			res.SnapshotDir = opts.SnapshotDir
		}
		raw, err := plan.backupContent(e)
		if err != nil {
			return res, err
		}
		if err := writeTarget(e.TargetPath, string(raw), e.Sensitivity, e.Exec); err != nil {
			return res, err
		}
		res.Restored++
		res.PerCategory[e.Category]++
		res.Outcomes = append(res.Outcomes, Outcome{Entry: e, Written: true})
	}
	return res, nil
}

// writeTarget writes a restored file owner-only when the registry marked it
// medium/high, so a secret never lands world-readable; low configs keep 0644.
// A file that was executable in the backup stays executable: git ignores a
// hook without the bit, and says so only in a hint nobody reads.
func writeTarget(path, content string, sens registry.Sensitivity, exec bool) error {
	perm := os.FileMode(0o644)
	if sens == registry.High || sens == registry.Medium {
		perm = 0o600
	}
	if exec {
		perm |= (perm & 0o444) >> 2 // r → x for each class that can read it
	}
	// Never loosen: a file someone made owner-only stays owner-only.
	if fi, err := os.Stat(path); err == nil {
		perm &^= 0o077 &^ fi.Mode().Perm()
	}
	return sys.WriteFileAs(path, content, perm)
}
