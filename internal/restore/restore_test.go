package restore

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/doguyilmaz/dothaven/internal/registry"
	"github.com/doguyilmaz/dothaven/internal/scan"
)

func TestClassify(t *testing.T) {
	if got := classify("anything "+scan.Marker, true, "x"); got != StatusRedacted {
		t.Errorf("redacted marker → %q, want redacted", got)
	}
	if got := classify("body", false, ""); got != StatusNew {
		t.Errorf("absent target → %q, want new", got)
	}
	if got := classify("same", true, "same"); got != StatusSame {
		t.Errorf("identical → %q, want same", got)
	}
	if got := classify("a", true, "b"); got != StatusConflict {
		t.Errorf("differ → %q, want conflict", got)
	}
}

func TestMatchTarget(t *testing.T) {
	m := map[string]mapping{
		"shell/.zshrc":     {target: "/home/u/.zshrc", category: "shell"},
		"ai/claude/skills": {target: "/home/u/.claude/skills", category: "ai", isDir: true},
	}
	cases := []struct {
		rel          string
		wantTarget   string
		wantCategory string
	}{
		{"shell/.zshrc", "/home/u/.zshrc", "shell"},
		{"ai/claude/skills/nested/a.md", "/home/u/.claude/skills/nested/a.md", "ai"},
		{"shell/.zshrc.local", "/home/u/.zshrc.local", "shell"},
		{"unknown/file", "", ""},
	}
	for _, c := range cases {
		gt, gc := matchTarget(c.rel, m)
		if gt != c.wantTarget || gc != c.wantCategory {
			t.Errorf("matchTarget(%q) = (%q,%q), want (%q,%q)", c.rel, gt, gc, c.wantTarget, c.wantCategory)
		}
	}
}

func TestTally(t *testing.T) {
	got := Tally([]Entry{
		{Status: StatusNew}, {Status: StatusNew}, {Status: StatusConflict}, {Status: StatusSame}, {Status: StatusRedacted},
	})
	want := Counts{New: 2, Conflict: 1, Same: 1, Redacted: 1}
	if got != want {
		t.Errorf("Tally = %+v, want %+v", got, want)
	}
}

func TestFilter(t *testing.T) {
	p := Plan{Entries: []Entry{
		{BackupPath: "shell/.zshrc", Category: "shell"},
		{BackupPath: "git/.gitconfig", Category: "git"},
		{BackupPath: "npm/.npmrc", Category: "npm"},
	}}
	only := Filter(p, []string{"shell", "git"}, nil)
	if len(only.Entries) != 2 || !reflect.DeepEqual(only.Categories, []string{"git", "shell"}) {
		t.Errorf("only filter = %+v", only)
	}
	skip := Filter(p, nil, []string{"npm"})
	if len(skip.Entries) != 2 {
		t.Errorf("skip filter kept %d, want 2", len(skip.Entries))
	}
}

func TestBuildPlanAndExecute(t *testing.T) {
	home := t.TempDir()
	backup := t.TempDir()

	// Backup tree: a new file, a conflicting file, an identical file, a redacted file.
	write(t, filepath.Join(backup, "shell/.zshrc"), "new content\n")
	write(t, filepath.Join(backup, "git/.gitconfig"), "backup version\n")
	write(t, filepath.Join(backup, "editor/.vimrc"), "same\n")
	write(t, filepath.Join(backup, "npm/.npmrc"), "token="+scan.Marker+"\n")

	// Live machine: gitconfig differs, vimrc identical, zshrc + npmrc absent.
	write(t, filepath.Join(home, ".gitconfig"), "live version\n")
	write(t, filepath.Join(home, ".vimrc"), "same\n")

	targets := []registry.BackupTarget{
		{Src: filepath.Join(home, ".zshrc"), Dest: "shell/.zshrc", Category: "shell"},
		{Src: filepath.Join(home, ".gitconfig"), Dest: "git/.gitconfig", Category: "git"},
		{Src: filepath.Join(home, ".vimrc"), Dest: "editor/.vimrc", Category: "editor"},
		{Src: filepath.Join(home, ".npmrc"), Dest: "npm/.npmrc", Category: "npm"},
	}

	plan, err := BuildPlan(backup, home, targets)
	if err != nil {
		t.Fatal(err)
	}
	got := Tally(plan.Entries)
	want := Counts{New: 1, Conflict: 1, Same: 1, Redacted: 1}
	if got != want {
		t.Fatalf("plan tally = %+v, want %+v", got, want)
	}

	// Default execute: writes new, skips conflict/same/redacted.
	res, err := Execute(plan, ExecuteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Restored != 1 {
		t.Errorf("default restore count = %d, want 1 (new only)", res.Restored)
	}
	if c := readF(t, filepath.Join(home, ".zshrc")); c != "new content\n" {
		t.Errorf("new file not restored: %q", c)
	}
	if c := readF(t, filepath.Join(home, ".gitconfig")); c != "live version\n" {
		t.Errorf("conflict should NOT be overwritten without --force, got %q", c)
	}

	// Force execute: snapshots then overwrites the conflict.
	snap := filepath.Join(t.TempDir(), "pre-restore")
	res2, err := Execute(plan, ExecuteOptions{Force: true, SnapshotDir: snap})
	if err != nil {
		t.Fatal(err)
	}
	if c := readF(t, filepath.Join(home, ".gitconfig")); c != "backup version\n" {
		t.Errorf("conflict not overwritten with --force, got %q", c)
	}
	if c := readF(t, filepath.Join(snap, "git/.gitconfig")); c != "live version\n" {
		t.Errorf("pre-restore snapshot missing prior content, got %q", c)
	}
	if res2.SnapshotDir == "" {
		t.Error("SnapshotDir should be reported when a conflict was snapshotted")
	}
	// redacted never written
	if _, err := os.Stat(filepath.Join(home, ".npmrc")); !os.IsNotExist(err) {
		t.Error("redacted entry must never be restored")
	}
}

func TestExecuteInteractiveResolver(t *testing.T) {
	home := t.TempDir()
	backup := t.TempDir()
	write(t, filepath.Join(backup, "git/.gitconfig"), "backup\n")
	write(t, filepath.Join(backup, "editor/.vimrc"), "backup\n")
	write(t, filepath.Join(home, ".gitconfig"), "live\n")
	write(t, filepath.Join(home, ".vimrc"), "live\n")

	targets := []registry.BackupTarget{
		{Src: filepath.Join(home, ".gitconfig"), Dest: "git/.gitconfig", Category: "git"},
		{Src: filepath.Join(home, ".vimrc"), Dest: "editor/.vimrc", Category: "editor"},
	}
	plan, _ := BuildPlan(backup, home, targets)

	// Resolver overwrites the first conflict it sees, then skips-all.
	calls := 0
	res, err := Execute(plan, ExecuteOptions{
		SnapshotDir: filepath.Join(t.TempDir(), "snap"),
		Resolve: func(e Entry, b, l string) ConflictAction {
			calls++
			if calls == 1 {
				return ActionOverwrite
			}
			return ActionSkipAll
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Restored != 1 {
		t.Errorf("restored = %d, want 1 (one overwrite)", res.Restored)
	}
	// After SkipAll, the resolver must not be called again for later conflicts.
	if calls != 2 {
		t.Errorf("resolver calls = %d, want 2 (skip-all stops further prompts)", calls)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readF(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
