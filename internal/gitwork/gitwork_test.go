package gitwork

import (
	"context"
	"os"
	"path/filepath"
	"sort"
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

// node_modules vendors thousands of repositories that are not the user's work,
// and reporting them would bury the ones that are.
//
// Asserted by walking a real tree, not by reading the skipDirs map back: a
// version of Find that never consulted the map would satisfy that, and it is
// the walk that has to skip them.
func TestFindSkipsVendoredRepositories(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{
		"mine/.git",
		"node_modules/somepkg/.git",
		"nested/vendor/lib/.git",
		"nested/deep/also-mine/.git",
	} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	got := Find(context.Background(), []string{root}, 6)
	var names []string
	for _, p := range got {
		rel, _ := filepath.Rel(root, p)
		names = append(names, rel)
	}
	sort.Strings(names)

	want := []string{"mine", "nested/deep/also-mine"}
	if len(names) != len(want) {
		t.Fatalf("found %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("found %v, want %v", names, want)
			break
		}
	}
}

// A repository deeper than the limit is not reported, so a home directory full
// of nested checkouts cannot turn a pre-wipe check into a minutes-long walk.
func TestFindRespectsTheDepthLimit(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a/b/c/d/e/.git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := Find(context.Background(), []string{root}, 2); len(got) != 0 {
		t.Errorf("depth 2 should not reach a/b/c/d/e, got %v", got)
	}
	if got := Find(context.Background(), []string{root}, 8); len(got) != 1 {
		t.Errorf("depth 8 should reach it, got %v", got)
	}
}

// Find must not descend into a repository's own .git, which contains
// directories that would otherwise look like more repositories.
func TestFindDoesNotRecurseIntoDotGit(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "repo/.git/modules/sub/.git"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := Find(context.Background(), []string{root}, 8)
	if len(got) != 1 || filepath.Base(got[0]) != "repo" {
		t.Errorf("got %v, want just the outer repo", got)
	}
}

func TestCountLines(t *testing.T) {
	for in, want := range map[string]int{"": 0, "  \n ": 0, "a": 1, "a\nb": 2, "a\nb\n": 2} {
		if got := countLines(in); got != want {
			t.Errorf("countLines(%q) = %d, want %d", strings.TrimSpace(in), got, want)
		}
	}
}
