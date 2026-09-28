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
// declined, and a category not picked stays on offer.
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

	// A category not picked is still on offer: a restore done in phases
	// must not find the later phases already "declined".
	plan, _ = BuildPlanWith(backupA, home, targets, lg)
	got := statuses(plan)
	if got["shell/.zshrc"] != StatusSame || got["git/.gitconfig"] != StatusNew {
		t.Fatalf("second run: %v", got)
	}
	// Unpicking a file by name, from a list that showed it, is a decision,
	// and that one is remembered.
	res, _ = Execute(plan, ExecuteOptions{Selected: func(Entry) bool { return false }, DeclineUnselected: true})
	lg.Record(plan.BackupID, res.Outcomes, time.Now())
	plan, _ = BuildPlanWith(backupA, home, targets, lg)
	if s := statuses(plan)["git/.gitconfig"]; s != StatusSkipped {
		t.Fatalf("declined by name = %s, want skipped", s)
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

// Off a terminal, a file that differs is kept, and kept on offer: nobody was
// asked, so nothing was decided.
func TestUnattendedConflictIsNotRemembered(t *testing.T) {
	home := t.TempDir()
	b := filepath.Join(t.TempDir(), "backup-box-1")
	mustWriteT(t, filepath.Join(b, "git", ".gitconfig"), "[user]\n\tname = Old\n")
	mustWriteT(t, filepath.Join(home, ".gitconfig"), "[user]\n\tname = New\n")
	targets := []registry.BackupTarget{{Src: filepath.Join(home, ".gitconfig"), Dest: "git/.gitconfig", Category: "git"}}
	lg := NewLedger()
	plan, _ := BuildPlanWith(b, home, targets, lg)
	res, _ := Execute(plan, ExecuteOptions{})
	if len(res.Outcomes) != 1 || !res.Outcomes[0].Kept || res.Outcomes[0].Declined {
		t.Fatalf("outcomes = %+v", res.Outcomes)
	}
	lg.Record(plan.BackupID, res.Outcomes, time.Now())
	plan, _ = BuildPlanWith(b, home, targets, lg)
	if s := statuses(plan)["git/.gitconfig"]; s != StatusConflict {
		t.Errorf("after an unattended run = %s, want conflict", s)
	}
}

// Restore never loosens a file's permissions: one made owner-only stays so.
func TestRestoreKeepsStricterPermissions(t *testing.T) {
	home := t.TempDir()
	b := filepath.Join(t.TempDir(), "backup-box-1")
	mustWriteT(t, filepath.Join(b, "shell", ".zshrc"), "alias a=1\n")
	live := filepath.Join(home, ".zshrc")
	os.WriteFile(live, []byte("alias b=2\n"), 0o600)
	os.Chmod(live, 0o600)
	targets := []registry.BackupTarget{{Src: live, Dest: "shell/.zshrc", Category: "shell", Sensitivity: registry.Low}}
	plan, _ := BuildPlanWith(b, home, targets, NewLedger())
	if _, err := Execute(plan, ExecuteOptions{Force: true, SnapshotDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(live)
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Errorf("perm = %o, want 600", got)
	}
}

// A backup made under another home folder lands with this machine's: in the
// plan (so the hash and "already applied" agree with what is on disk) and in
// the file written. Only whole path components change.
func TestHomeRewriting(t *testing.T) {
	rw := HomeRewriter("/Users/dogu", "/home/dogu.yilmaz")
	in := "export PATH=/Users/dogu/bin:$PATH\nsrc=/Users/doguyilmaz/x\nend=/Users/dogu\n"
	want := "export PATH=/home/dogu.yilmaz/bin:$PATH\nsrc=/Users/doguyilmaz/x\nend=/home/dogu.yilmaz\n"
	if got := string(rw([]byte(in))); got != want {
		t.Errorf("rewrite =\n%s\nwant\n%s", got, want)
	}
	if bin := []byte("\x00\x01/Users/dogu/x"); string(rw(bin)) != string(bin) {
		t.Error("a binary file was rewritten")
	}
	if HomeRewriter("/Users/dogu", "/Users/dogu") != nil || HomeRewriter("", "/h") != nil {
		t.Error("nothing to rewrite should give nil")
	}

	home := t.TempDir()
	b := filepath.Join(t.TempDir(), "backup-box-1")
	mustWriteT(t, filepath.Join(b, "shell", ".zshrc"), "export PATH=/Users/dogu/bin\n")
	targets := []registry.BackupTarget{{Src: filepath.Join(home, ".zshrc"), Dest: "shell/.zshrc", Category: "shell"}}
	lg := NewLedger()
	plan, _ := BuildPlanRewriting(b, home, targets, lg, HomeRewriter("/Users/dogu", home))
	if len(plan.Entries) != 1 || !plan.Entries[0].Rewritten {
		t.Fatalf("plan = %+v", plan.Entries)
	}
	res, err := Execute(plan, ExecuteOptions{})
	if err != nil || res.Restored != 1 {
		t.Fatalf("restore: %+v %v", res, err)
	}
	got, _ := os.ReadFile(filepath.Join(home, ".zshrc"))
	if string(got) != "export PATH="+home+"/bin\n" {
		t.Errorf("written = %q", got)
	}
	lg.Record(plan.BackupID, res.Outcomes, time.Now())
	plan, _ = BuildPlanRewriting(b, home, targets, lg, HomeRewriter("/Users/dogu", home))
	if s := statuses(plan)["shell/.zshrc"]; s != StatusSame {
		t.Errorf("second run = %s, want applied", s)
	}
}
