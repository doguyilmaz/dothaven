package chezmoi

import (
	"strings"
	"testing"
)

func TestRepoURL(t *testing.T) {
	if got := RepoURL("octocat"); got != "git@github.com:octocat/dotfiles.git" {
		t.Errorf("RepoURL(octocat) = %q", got)
	}
	if got := RepoURL(""); !strings.Contains(got, "<you>") {
		t.Errorf("empty user should leave a placeholder, got %q", got)
	}
}

func TestPlanInitAllDone(t *testing.T) {
	steps := PlanInit(InitState{ChezmoiInstalled: true, AgeKeyConfigured: true, SourceInitialized: true})
	if !IsReady(steps) {
		t.Fatal("all-satisfied state should be ready")
	}
	for _, s := range steps {
		if s.Command != "" {
			t.Errorf("done step %q should carry no command, got %q", s.ID, s.Command)
		}
	}
}

func TestPlanInitNoneDone(t *testing.T) {
	steps := PlanInit(InitState{User: "octocat"})
	if IsReady(steps) {
		t.Fatal("empty state should not be ready")
	}
	cmds := map[string]string{}
	for _, s := range steps {
		cmds[s.ID] = s.Command
	}
	if cmds["chezmoi"] != "brew install chezmoi" {
		t.Errorf("chezmoi step command = %q", cmds["chezmoi"])
	}
	if !strings.Contains(cmds["age-key"], "age-keygen") {
		t.Errorf("age-key step command = %q", cmds["age-key"])
	}
	if cmds["source"] != "chezmoi init git@github.com:octocat/dotfiles.git" {
		t.Errorf("source step command = %q", cmds["source"])
	}
	// The key-loss warning must be present.
	for _, s := range steps {
		if s.ID == "age-key" && !strings.Contains(s.Note, "unrecoverable") {
			t.Errorf("age-key step missing loss warning: %q", s.Note)
		}
	}
}
