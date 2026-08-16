package gitwork

import (
	"context"
	"strings"
	"testing"
)

// fakeGit answers each git subcommand from a table, so the logic can be tested
// without building repositories on disk.
func fakeGit(out map[string]string) Runner {
	return func(_ context.Context, _ string, args ...string) (string, error) {
		return out[args[0]], nil
	}
}

func TestRepoWithNoRemoteIsAlwaysAtRisk(t *testing.T) {
	// Clean tree, nothing stashed — and still unrecoverable, because there is
	// nowhere else for any of it to be.
	r := inspectOne(context.Background(), fakeGit(map[string]string{
		"status":   "",
		"remote":   "",
		"rev-list": "412",
		"stash":    "",
	}), "/x")
	if r.HasRemote {
		t.Fatal("no remote configured should read as HasRemote=false")
	}
	if !r.AtRisk() {
		t.Error("a repository with no remote holds work that exists nowhere else")
	}
	if r.Unsaved != 412 {
		t.Errorf("Unsaved = %d, want 412", r.Unsaved)
	}
}

func TestFullyPushedRepoIsNotAtRisk(t *testing.T) {
	r := inspectOne(context.Background(), fakeGit(map[string]string{
		"status":   "",
		"remote":   "origin",
		"rev-list": "0",
		"stash":    "",
	}), "/x")
	if r.AtRisk() {
		t.Errorf("clean, pushed repo flagged: %+v", r)
	}
}

// The check this replaced asked "is the branch ahead of its upstream", which
// missed commits on branches with no upstream and misreported branches that
// were fully pushed but untracked. `--branches --not --remotes` asks the real
// question: is this commit anywhere other than here?
func TestUnsavedCountsCommitsOnNoRemote(t *testing.T) {
	r := inspectOne(context.Background(), fakeGit(map[string]string{
		"status":   "",
		"remote":   "origin",
		"rev-list": "3",
		"stash":    "",
	}), "/x")
	if r.Unsaved != 3 || !r.AtRisk() {
		t.Errorf("want 3 unsaved and at risk, got %+v", r)
	}
}

func TestStashesAloneAreAtRisk(t *testing.T) {
	// A stash is not carried by pushing a branch, which is exactly why it gets
	// lost in a migration.
	r := inspectOne(context.Background(), fakeGit(map[string]string{
		"status":   "",
		"remote":   "origin",
		"rev-list": "0",
		"stash":    "stash@{0}: WIP\nstash@{1}: WIP",
	}), "/x")
	if r.Stashes != 2 {
		t.Fatalf("Stashes = %d, want 2", r.Stashes)
	}
	if !r.AtRisk() {
		t.Error("stashed work exists only on this machine")
	}
}

func TestDirtyCountsChangedFiles(t *testing.T) {
	r := inspectOne(context.Background(), fakeGit(map[string]string{
		"status":   " M a.go\n?? b.go\nA  c.go",
		"remote":   "origin",
		"rev-list": "0",
		"stash":    "",
	}), "/x")
	if r.Dirty != 3 {
		t.Errorf("Dirty = %d, want 3", r.Dirty)
	}
}

func TestGitFailureIsReportedNotSwallowed(t *testing.T) {
	failing := func(_ context.Context, _ string, args ...string) (string, error) {
		return "", context.DeadlineExceeded
	}
	r := inspectOne(context.Background(), failing, "/x")
	if r.Err == "" {
		t.Error("a repo git could not read must say so, not appear clean")
	}
}

func TestInspectReturnsOnlyRepositoriesWorthActingOn(t *testing.T) {
	clean := fakeGit(map[string]string{"status": "", "remote": "origin", "rev-list": "0", "stash": ""})
	got := Inspect(context.Background(), clean, []string{"/a", "/b"}, nil)
	if len(got) != 0 {
		t.Errorf("clean repos should not be reported, got %d", len(got))
	}
}

func TestFindSkipsDependencyTrees(t *testing.T) {
	// node_modules vendors thousands of repos that are not the user's work.
	for _, name := range []string{"node_modules", "vendor", "Library", ".cache"} {
		if !skipDirs[name] {
			t.Errorf("%s should be skipped when looking for repositories", name)
		}
	}
}

func TestCountLines(t *testing.T) {
	for in, want := range map[string]int{"": 0, "  \n ": 0, "a": 1, "a\nb": 2, "a\nb\n": 2} {
		if got := countLines(in); got != want {
			t.Errorf("countLines(%q) = %d, want %d", strings.TrimSpace(in), got, want)
		}
	}
}
