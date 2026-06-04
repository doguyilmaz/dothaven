package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/doguyilmaz/dothaven/internal/chezmoi"
	"github.com/doguyilmaz/dothaven/internal/registry"
	"github.com/doguyilmaz/dothaven/internal/snapshot"
)

func TestFuzzyMatch(t *testing.T) {
	cases := []struct {
		query, section string
		want           bool
	}{
		{"shell", "shell.zshrc", true},
		{"zshrc", "shell.zshrc", true}, // matches a dotted part
		{"SHELL", "shell.zshrc", true}, // case-insensitive
		{"brew", "apps.brew.formulae", true},
		{"xyz", "shell.zshrc", false},
	}
	for _, c := range cases {
		if got := fuzzyMatch(c.query, c.section); got != c.want {
			t.Errorf("fuzzyMatch(%q,%q) = %v, want %v", c.query, c.section, got, c.want)
		}
	}
}

func TestFormatSection(t *testing.T) {
	content := "alias ll='ls -la'"
	s := snapshot.Section{
		Pairs: map[string]string{"theme": "dark"},
		Items: []snapshot.Item{{Raw: "a.md", Columns: []string{"a.md"}}, {Raw: "pkg@1.0", Columns: []string{"pkg", "1.0"}}},
	}
	out := formatSection("ai.gemini", s)
	for _, want := range []string{"[ai.gemini]", "theme = dark", "a.md", "pkg  1.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("formatSection missing %q in:\n%s", want, out)
		}
	}

	withContent := formatSection("shell.zshrc", snapshot.Section{Content: &content})
	if !strings.Contains(withContent, "alias ll") || !strings.Contains(withContent, "---") {
		t.Errorf("formatSection content render: %s", withContent)
	}
}

func TestBackupGroups(t *testing.T) {
	targets := []registry.BackupTarget{
		{Category: "shell"}, {Category: "shell"}, {Category: "git"},
		{Category: "cloud", Sensitivity: registry.High},
		{Category: "cloud"},
	}
	got := backupGroups(targets)
	// sorted by category: cloud, git, shell
	if len(got) != 3 {
		t.Fatalf("groups = %d, want 3", len(got))
	}
	if got[0].Name != "cloud" || got[0].Count != 2 || !got[0].Encrypted {
		t.Errorf("cloud group = %+v (want count 2, encrypted)", got[0])
	}
	if got[2].Name != "shell" || got[2].Count != 2 || got[2].Encrypted {
		t.Errorf("shell group = %+v (want count 2, not encrypted)", got[2])
	}
}

func TestFormatCategories(t *testing.T) {
	got := formatCategories(map[string]int{"shell": 3, "git": 2, "npm": 1})
	if got != "git (2), npm (1), shell (3)" { // sorted by category
		t.Errorf("formatCategories = %q", got)
	}
	if formatCategories(map[string]int{}) != "" {
		t.Error("empty map should format to empty string")
	}
}

func TestPlanHelpers(t *testing.T) {
	plan := []chezmoi.PlanItem{
		{ID: "shell.zshrc", Src: "/h/.zshrc"},
		{ID: "secrets.gnupg", Src: "/h/.gnupg"},
	}
	if !planHasSrc(plan, "/h/.zshrc") || planHasSrc(plan, "/nope") {
		t.Error("planHasSrc wrong")
	}
	if !planHasID(plan, "secrets.gnupg") || planHasID(plan, "nope") {
		t.Error("planHasID wrong")
	}
	pruned := removeByID(plan, "secrets.gnupg")
	if len(pruned) != 1 || pruned[0].ID != "shell.zshrc" {
		t.Errorf("removeByID = %+v", pruned)
	}
}

func TestLabelAndSortedKeys(t *testing.T) {
	if got := label("/a/b/box-20240101.json"); got != "box-20240101" {
		t.Errorf("label = %q", got)
	}
	got := sortedStringKeys(map[string]string{"b": "", "a": "", "c": ""})
	if !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("sortedStringKeys = %v", got)
	}
}

func TestNewestJSON(t *testing.T) {
	dir := t.TempDir()
	// Create three .json files with increasing mtimes and a non-json decoy.
	for i, name := range []string{"old.json", "mid.json", "new.json"} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		mt := time.Date(2024, 1, 1+i, 0, 0, 0, 0, time.UTC)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := newestJSON(dir, 2)
	if len(got) != 2 || filepath.Base(got[0]) != "new.json" || filepath.Base(got[1]) != "mid.json" {
		t.Errorf("newestJSON = %v (want new.json, mid.json)", got)
	}
	if newestJSON(filepath.Join(dir, "missing"), 2) != nil {
		t.Error("missing dir should yield nil")
	}
}

func TestLatestBackupAndAge(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"backup-box-20240101000000", "backup-box-20240202000000", "reports"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// An archive is ignored (can't be diffed without extraction).
	if err := os.WriteFile(filepath.Join(dir, "backup-box-20240303000000.tar.gz"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := latestBackup(dir)
	if filepath.Base(got) != "backup-box-20240202000000" {
		t.Errorf("latestBackup = %q, want the newest backup dir", got)
	}
	if latestBackup(filepath.Join(dir, "missing")) != "" {
		t.Error("missing dir → empty")
	}
	if latestBackup(t.TempDir()) != "" {
		t.Error("no backups → empty")
	}

	// backupAge is mtime-relative; a freshly made dir reads as minutes.
	if age := backupAge(got); age == "unknown" {
		t.Errorf("backupAge should resolve for an existing dir, got %q", age)
	}
	if backupAge(filepath.Join(dir, "nope")) != "unknown" {
		t.Error("missing path → unknown age")
	}
}
