package restore

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/doguyilmaz/dothaven/internal/registry"
)

// The whole lifecycle: first run writes; a second run shows it applied; an
// edit afterwards reads as "changed since", not as a plain conflict; a newer
// backup over an untouched file reads as an update; a declined file stays
// declined.
func TestLedgerLifecycle(t *testing.T) {
	home := t.TempDir()
	backupA := filepath.Join(t.TempDir(), "backup-box-1")
	mustWriteT(t, filepath.Join(backupA, "shell", ".zshrc"), "alias a=1\n")
	mustWriteT(t, filepath.Join(backupA, "git", ".gitconfig"), "[user]\n")
	targets := []registry.BackupTarget{
		{Src: filepath.Join(home, ".zshrc"), Dest: "shell/.zshrc", Category: "shell"},
		{Src: filepath.Join(home, ".gitconfig"), Dest: "git/.gitconfig", Category: "git"},
	}
	lg := NewLedger()

	plan, _ := BuildPlanWith(backupA, home, targets, lg)
	res, err := Execute(plan, ExecuteOptions{Selected: func(e Entry) bool { return e.Category == "shell" }})
	if err != nil {
		t.Fatal(err)
	}
	lg.Record(plan.BackupID, res.Outcomes, time.Now())
	if res.Restored != 1 {
		t.Fatalf("Restored = %d", res.Restored)
	}

	plan, _ = BuildPlanWith(backupA, home, targets, lg)
	got := statuses(plan)
	if got["shell/.zshrc"] != StatusSame || got["git/.gitconfig"] != StatusSkipped {
		t.Fatalf("second run: %v", got)
	}
	for _, e := range plan.Entries {
		if e.BackupPath == "shell/.zshrc" && e.AppliedAt.IsZero() {
			t.Error("applied file should carry when it was applied")
		}
	}
	// Non-interactive re-run does not resurrect the declined file.
	res, _ = Execute(plan, ExecuteOptions{})
	if res.Restored != 0 {
		t.Errorf("declined file was written on a re-run: %+v", res)
	}

	// The user edits the restored file.
	os.WriteFile(filepath.Join(home, ".zshrc"), []byte("alias mine=1\n"), 0o644)
	plan, _ = BuildPlanWith(backupA, home, targets, lg)
	if s := statuses(plan)["shell/.zshrc"]; s != StatusChanged {
		t.Errorf("edited after apply = %s, want changed", s)
	}

	// Put it back as applied, then a newer backup arrives.
	os.WriteFile(filepath.Join(home, ".zshrc"), []byte("alias a=1\n"), 0o644)
	backupB := filepath.Join(t.TempDir(), "backup-box-2")
	mustWriteT(t, filepath.Join(backupB, "shell", ".zshrc"), "alias a=2\n")
	plan, _ = BuildPlanWith(backupB, home, targets, lg)
	if s := statuses(plan)["shell/.zshrc"]; s != StatusUpdate {
		t.Errorf("untouched file vs newer backup = %s, want update", s)
	}
	// An update is written without asking, and the old copy is kept.
	snap := t.TempDir()
	res, _ = Execute(plan, ExecuteOptions{SnapshotDir: snap})
	if res.Restored != 1 {
		t.Errorf("update not applied: %+v", res)
	}
	if b, _ := os.ReadFile(filepath.Join(snap, "shell", ".zshrc")); string(b) != "alias a=1\n" {
		t.Errorf("update did not snapshot the old copy: %q", b)
	}
}

func TestLedgerSaveLoad(t *testing.T) {
	p := filepath.Join(t.TempDir(), "applied.json")
	lg := NewLedger()
	lg.Applied["/h/.zshrc"] = AppliedFile{SHA256: "abc", Backup: "b1", Dest: "shell/.zshrc"}
	if err := lg.Save(p); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Errorf("ledger mode %o", fi.Mode().Perm())
	}
	if got := LoadLedger(p); got.Applied["/h/.zshrc"].SHA256 != "abc" {
		t.Errorf("round trip lost data: %+v", got)
	}
	os.WriteFile(p, []byte("{not json"), 0o600)
	if got := LoadLedger(p); got == nil || len(got.Applied) != 0 {
		t.Error("a corrupt ledger should read as empty, not fail")
	}
}

func statuses(p Plan) map[string]Status {
	m := map[string]Status{}
	for _, e := range p.Entries {
		m[e.BackupPath] = e.Status
	}
	return m
}

func mustWriteT(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
