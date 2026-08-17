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

func noteText(p *plan) string { return strings.Join(p.notes, "\n") }

// fullText is everything the user actually reads: commands, reasons and notes.
func fullText(p *plan) string {
	var b strings.Builder
	for _, s := range p.steps {
		b.WriteString(s.cmd + "\n" + s.why + "\n")
	}
	b.WriteString(p.reason + "\n" + noteText(p))
	return b.String()
}

func TestGuideBackupStaysLocal(t *testing.T) {
	p, err := runGuide(machineFacts{}, scripted(t, "backup", "frontend", "local"))
	if err != nil {
		t.Fatal(err)
	}
	got := planText(p)
	if !strings.Contains(got, "dothaven backup") {
		t.Errorf("want a backup, got:\n%s", got)
	}
	if strings.Contains(got, "chezmoi") {
		t.Errorf("nothing has to leave this Mac, so chezmoi is setup for nothing:\n%s", got)
	}
}

// The profile question has to change something, or it is a question for its
// own sake. Backend work has service configs a plain backup misses.
func TestProfileChangesThePlanNotJustTheProse(t *testing.T) {
	backend, _ := runGuide(machineFacts{}, scripted(t, "backup", "backend", "local"))
	frontend, _ := runGuide(machineFacts{}, scripted(t, "backup", "frontend", "local"))

	if !strings.Contains(planText(backend), "services export") {
		t.Errorf("backend work needs Homebrew service configs:\n%s", planText(backend))
	}
	if strings.Contains(planText(frontend), "services export") {
		t.Errorf("frontend work does not:\n%s", planText(frontend))
	}
}

// Every profile must warn about what does NOT travel. That is the thing people
// discover on the new machine, when it is expensive.
func TestEveryProfileSaysWhatDoesNotTravel(t *testing.T) {
	for _, profile := range []string{"backend", "frontend", "mobile", "devops", "data", "all"} {
		notes := profileNotes(profile)
		if len(notes) == 0 {
			t.Errorf("%s: no notes at all", profile)
			continue
		}
		joined := strings.ToLower(strings.Join(notes, " "))
		var statesAnExclusion bool
		for _, marker := range []string{"not ", "never", "excluded", "out of scope", "do not"} {
			if strings.Contains(joined, marker) {
				statesAnExclusion = true
			}
		}
		if !statesAnExclusion {
			t.Errorf("%s: notes never say what is excluded: %v", profile, notes)
		}
	}
}

// Signing certificates are the mobile-specific thing that stops a build on the
// new machine, and nothing else in the tool would mention them.
func TestMobileProfileWarnsAboutSigningCertificates(t *testing.T) {
	p, err := runGuide(machineFacts{}, scripted(t, "backup", "mobile", "local"))
	if err != nil {
		t.Fatal(err)
	}
	notes := strings.ToLower(noteText(p))
	for _, want := range []string{"keychain", "simulator"} {
		if !strings.Contains(notes, want) {
			t.Errorf("mobile notes should mention %q:\n%s", want, noteText(p))
		}
	}
}

func TestDevopsProfileWarnsAboutCredentialsInConfigs(t *testing.T) {
	p, _ := runGuide(machineFacts{}, scripted(t, "backup", "devops", "local"))
	if !strings.Contains(strings.ToLower(noteText(p)), "credential") {
		t.Errorf("kubeconfigs and cloud CLI configs hold credentials:\n%s", noteText(p))
	}
}

// The most important branch: credentials are travelling and encryption is not
// ready. Exporting here would write them in the clear.
func TestGuideRefusesToExportSecretsBeforeEncryptionExists(t *testing.T) {
	p, err := runGuide(machineFacts{chezmoiInstalled: true, ageReady: false},
		scripted(t, "clone", "devops", "old", "yes"))
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

func TestGuideExportsWhenEverythingIsReady(t *testing.T) {
	p, err := runGuide(machineFacts{chezmoiInstalled: true, ageReady: true},
		scripted(t, "clone", "backend", "old", "yes"))
	if err != nil {
		t.Fatal(err)
	}
	got := planText(p)
	if !strings.Contains(got, "chezmoi-export --apply") {
		t.Errorf("want the real export, got:\n%s", got)
	}
	if strings.Index(got, "chezmoi-export\n") > strings.Index(got, "--apply") {
		t.Error("the preview should come before the apply")
	}
}

// Without credentials there is nothing to encrypt, so chezmoi is setup for
// nothing — a plain backup carried across is simpler.
func TestGuideSkipsChezmoiWhenNoCredentialsTravel(t *testing.T) {
	p, _ := runGuide(machineFacts{}, scripted(t, "clone", "frontend", "old", "no"))
	if strings.Contains(planText(p), "chezmoi") {
		t.Errorf("no credentials means no encryption to set up:\n%s", planText(p))
	}
}

func TestGuideOnTheNewMachinePicksBySource(t *testing.T) {
	withRepo, _ := runGuide(machineFacts{chezmoiInstalled: true, sourceReady: true},
		scripted(t, "clone", "all", "new"))
	if !strings.Contains(planText(withRepo), "migrate --dry-run") {
		t.Errorf("a chezmoi repo means migrate:\n%s", planText(withRepo))
	}

	withBackup, _ := runGuide(machineFacts{latestBackup: "/data/backup-x"},
		scripted(t, "clone", "all", "new"))
	got := planText(withBackup)
	if !strings.Contains(got, "restore --dry-run /data/backup-x") {
		t.Errorf("a backup folder means restore, with the path it found:\n%s", got)
	}

	withNothing, _ := runGuide(machineFacts{}, scripted(t, "clone", "all", "new"))
	if strings.Contains(planText(withNothing), "dothaven restore ") {
		t.Errorf("nothing to restore from — do not suggest restoring:\n%s", planText(withNothing))
	}
}

// The wipe path holds the only irreversible mistake, so `ready` comes first
// every time: a backup taken after the disk is erased is not a backup.
func TestGuideChecksForUnsavedWorkBeforeAnythingElse(t *testing.T) {
	for _, after := range []string{"same", "remote"} {
		p, err := runGuide(machineFacts{chezmoiInstalled: true, ageReady: true},
			scripted(t, "wipe", after))
		if err != nil {
			t.Fatal(err)
		}
		if p.steps[0].cmd != "dothaven ready" {
			t.Errorf("%s: first step is %q, want `dothaven ready`", after, p.steps[0].cmd)
		}
	}
}

// Reinstalling the same machine is the case where the backup is on the disk
// being erased — worth saying out loud.
func TestGuideWipeSameMachineWarnsTheBackupIsOnThatDisk(t *testing.T) {
	p, _ := runGuide(machineFacts{}, scripted(t, "wipe", "same"))
	var warned bool
	for _, s := range p.steps {
		if s.warn && strings.Contains(strings.ToLower(s.cmd), "off this machine") {
			warned = true
		}
	}
	if !warned {
		t.Errorf("want a warning to copy the backup off the disk:\n%s", planText(p))
	}
}

func TestGuideHealthCoversAllThreeKinds(t *testing.T) {
	p, err := runGuide(machineFacts{}, scripted(t, "health", "all"))
	if err != nil {
		t.Fatal(err)
	}
	got := planText(p)
	for _, want := range []string{"dothaven check", "dothaven scan", "dothaven ready"} {
		if !strings.Contains(got, want) {
			t.Errorf("health should include %q:\n%s", want, got)
		}
	}
}

func TestGuideCompareNeedsSnapshotsFromBothMachines(t *testing.T) {
	p, err := runGuide(machineFacts{}, scripted(t, "compare"))
	if err != nil {
		t.Fatal(err)
	}
	got := fullText(p)
	if !strings.Contains(got, "BOTH") {
		t.Errorf("comparing machines needs a snapshot from each:\n%s", got)
	}
}

// A private repo that holds credentials must not be built before encryption
// exists: git remembers what was committed in the clear.
func TestGuideRepoWarnsBeforeCommittingSecretsUnencrypted(t *testing.T) {
	p, err := runGuide(machineFacts{chezmoiInstalled: true, sourceReady: true, ageReady: false},
		scripted(t, "repo", "yes"))
	if err != nil {
		t.Fatal(err)
	}
	var warned bool
	for _, s := range p.steps {
		if s.warn && strings.Contains(strings.ToLower(s.why), "git remembers") {
			warned = true
		}
	}
	if !warned {
		t.Errorf("want a warning about committing secrets in plain text:\n%s", planText(p))
	}
	if !strings.Contains(strings.ToLower(noteText(p)), "private") {
		t.Errorf("the repo must be private:\n%s", noteText(p))
	}
}

// Every plan has to explain itself; advice you cannot check is advice you
// cannot trust.
func TestEveryPlanExplainsItself(t *testing.T) {
	paths := [][]string{
		{"backup", "backend", "local"},
		{"backup", "mobile", "portable"},
		{"clone", "data", "old", "no"},
		{"clone", "all", "new"},
		{"wipe", "same"},
		{"health", "devops"},
		{"compare"},
		{"repo", "no"},
		{"see", "frontend"},
		{"see", "all"},
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
