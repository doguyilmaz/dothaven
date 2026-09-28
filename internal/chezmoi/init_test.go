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

func TestPlanInitKeyWithoutConfig(t *testing.T) {
	steps := PlanInit(InitState{ChezmoiInstalled: true, AgeKeyExists: true, SourceInitialized: true})
	if IsReady(steps) {
		t.Fatal("a key that chezmoi.toml does not use is not ready")
	}
	age := steps[1]
	if age.Command != "" {
		t.Errorf("the key exists; making another one would be wrong: %q", age.Command)
	}
	if !strings.Contains(age.Note, "chezmoi.toml") {
		t.Errorf("the note should say what is missing: %q", age.Note)
	}
}

func TestAgeConfig(t *testing.T) {
	const id, rcpt = "/Users/me/.config/chezmoi/key.txt", "age1abc"
	got, ok := AgeConfig("", id, rcpt)
	want := "encryption = \"age\"\n[age]\n    identity = \"/Users/me/.config/chezmoi/key.txt\"\n    recipient = \"age1abc\"\n"
	if !ok || got != want {
		t.Errorf("new file:\n%s", got)
	}

	// A table already in the file: the top-level key must come before it.
	got, ok = AgeConfig("[data]\n    email = \"me@example.com\"", id, rcpt)
	if !ok || !strings.HasPrefix(got, "encryption = \"age\"\n[data]\n") || !strings.HasSuffix(got, "recipient = \"age1abc\"\n") {
		t.Errorf("existing table:\n%s", got)
	}

	for _, existing := range []string{"encryption = \"gpg\"\n", "[age]\n    identity = \"x\"\n", "  encryption=\"age\""} {
		if _, ok := AgeConfig(existing, id, rcpt); ok {
			t.Errorf("changed a file that already sets encryption:\n%s", existing)
		}
	}
}
