package cli

import (
	"strings"
	"testing"

	"github.com/doguyilmaz/dothaven/internal/tui"
)

// scripted answers the questions in order, and fails loudly if the guide asks
// more than the script covers — an unanswered question means a path nobody
// checked.
func scripted(t *testing.T, answers ...string) asker {
	t.Helper()
	i := 0
	return func(title, _ string, _ []tui.Choice) (string, error) {
		if i >= len(answers) {
			t.Fatalf("unscripted question: %q", title)
		}
		a := answers[i]
		i++
		return a, nil
	}
}

func planText(p *plan) string {
	var b strings.Builder
	for _, s := range p.steps {
		b.WriteString(s.cmd)
		b.WriteString("\n")
	}
	return b.String()
}

func TestGuideKeepsALocalCopyLocal(t *testing.T) {
	p, err := runGuide(machineFacts{}, scripted(t, "save", "local"))
	if err != nil {
		t.Fatal(err)
	}
	got := planText(p)
	if !strings.Contains(got, "dothaven backup") {
		t.Errorf("want a plain backup, got:\n%s", got)
	}
	if strings.Contains(got, "chezmoi") {
		t.Errorf("nothing has to leave this Mac, so chezmoi is setup for nothing:\n%s", got)
	}
}

// The most important branch: secrets are travelling and encryption isn't ready.
// Exporting here would write credentials in the clear.
func TestGuideRefusesToExportSecretsBeforeEncryptionExists(t *testing.T) {
	p, err := runGuide(machineFacts{chezmoiInstalled: true, ageReady: false},
		scripted(t, "save", "remote", "yes"))
	if err != nil {
		t.Fatal(err)
	}
	got := planText(p)
	if strings.Contains(got, "--apply") {
		t.Fatalf("suggested applying an export with no encryption configured:\n%s", got)
	}
	if !strings.Contains(got, "dothaven init") {
		t.Errorf("want init first, got:\n%s", got)
	}
}

// An age key that exists only on this Mac is a single point of total loss:
// everything encrypted with it becomes unrecoverable if the Mac does.
func TestGuidePutsTheAgeKeyBackupBeforeEncrypting(t *testing.T) {
	p, err := runGuide(machineFacts{chezmoiInstalled: true, ageReady: true},
		scripted(t, "save", "remote", "yes", "no"))
	if err != nil {
		t.Fatal(err)
	}
	if !p.steps[0].warn {
		t.Fatalf("the key warning must come first, got %q", p.steps[0].cmd)
	}
	if strings.Contains(planText(p), "--apply") {
		t.Error("should not offer to encrypt anything until the key is safe")
	}
}

func TestGuideExportsWhenEverythingIsReady(t *testing.T) {
	p, err := runGuide(machineFacts{chezmoiInstalled: true, ageReady: true},
		scripted(t, "save", "remote", "yes", "yes"))
	if err != nil {
		t.Fatal(err)
	}
	got := planText(p)
	if !strings.Contains(got, "dothaven chezmoi-export --apply") {
		t.Errorf("want the real export, got:\n%s", got)
	}
	// Preview before write, every time.
	if strings.Index(got, "chezmoi-export\n") > strings.Index(got, "--apply") {
		t.Error("the preview should come before the apply")
	}
}

func TestGuideInstallsChezmoiWhenMissing(t *testing.T) {
	p, err := runGuide(machineFacts{}, scripted(t, "setup", "chezmoi"))
	if err != nil {
		t.Fatal(err)
	}
	got := planText(p)
	for _, want := range []string{"brew install chezmoi", "chezmoi init", "migrate --dry-run"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Index(got, "--dry-run") > strings.Index(got, "dothaven migrate\n") {
		t.Error("the dry run should come before the real migrate")
	}
}

// Telling someone to restore from a backup that isn't there is worse than
// saying so — they'd run it, see nothing, and not know why.
func TestGuideSaysSoWhenThereIsNoBackup(t *testing.T) {
	p, err := runGuide(machineFacts{latestBackup: ""}, scripted(t, "setup", "backup"))
	if err != nil {
		t.Fatal(err)
	}
	if !p.steps[0].warn {
		t.Fatalf("want a warning, got %q", p.steps[0].cmd)
	}
	if strings.Contains(planText(p), "dothaven restore ") {
		t.Error("suggested restoring from a backup that does not exist")
	}
}

func TestGuideRestoresFromTheBackupItFound(t *testing.T) {
	p, err := runGuide(machineFacts{latestBackup: "/data/backup-mac-123"},
		scripted(t, "setup", "backup"))
	if err != nil {
		t.Fatal(err)
	}
	got := planText(p)
	if !strings.Contains(got, "/data/backup-mac-123") {
		t.Errorf("the path it found should be in the command, got:\n%s", got)
	}
	if !strings.Contains(got, "--dry-run") {
		t.Errorf("restore writes to $HOME; preview first:\n%s", got)
	}
}

func TestGuideAuditSuggestsAGateForAProject(t *testing.T) {
	p, err := runGuide(machineFacts{}, scripted(t, "audit", "here", "console"))
	if err != nil {
		t.Fatal(err)
	}
	got := planText(p)
	if !strings.Contains(got, "dothaven scan .") {
		t.Errorf("want a scan of this folder, got:\n%s", got)
	}
	if !strings.Contains(got, "git commit") {
		t.Errorf("a project scan is worth wiring into a commit, got:\n%s", got)
	}
}

func TestGuideInspectNeedsSomethingToCompareAgainst(t *testing.T) {
	p, err := runGuide(machineFacts{latestBackup: ""}, scripted(t, "inspect", "backup"))
	if err != nil {
		t.Fatal(err)
	}
	if !p.steps[0].warn {
		t.Errorf("want a warning when there is no backup, got %q", p.steps[0].cmd)
	}
}

// Every plan has to explain itself; advice you can't check is advice you can't
// trust.
func TestEveryPlanExplainsItself(t *testing.T) {
	paths := [][]string{
		{"save", "local"},
		{"save", "remote", "no"},
		{"save", "remote", "yes", "yes"},
		{"setup", "chezmoi"},
		{"setup", "unsure"},
		{"inspect", "snapshot"},
		{"inspect", "two"},
		{"audit", "home", "file"},
	}
	for _, answers := range paths {
		p, err := runGuide(machineFacts{latestBackup: "/b"}, scripted(t, answers...))
		if err != nil {
			t.Fatalf("%v: %v", answers, err)
		}
		if p.reason == "" {
			t.Errorf("%v: no reason given", answers)
		}
		if len(p.steps) == 0 {
			t.Errorf("%v: no steps", answers)
		}
	}
}
