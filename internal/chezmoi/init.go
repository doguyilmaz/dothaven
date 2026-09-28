package chezmoi

import (
	"fmt"
	"regexp"
	"strings"
)

// InitState is the probed readiness of the chezmoi + age prerequisites.
type InitState struct {
	ChezmoiInstalled  bool
	AgeKeyConfigured  bool
	AgeKeyExists      bool // key.txt is there, whether or not chezmoi.toml uses it
	SourceInitialized bool
	User              string // GitHub login, for the repo URL suggestion
}

// InitStep is one ordered prerequisite with its status and remediation command.
type InitStep struct {
	ID      string // "chezmoi" | "age-key" | "source"
	Title   string
	Done    bool
	Command string
	Note    string
}

const keyPath = "~/.config/chezmoi/key.txt"

// RepoURL is the private chezmoi-source repo URL (the `dotfiles` name matches
// chezmoi's default convention).
func RepoURL(user string) string {
	if user == "" {
		user = "<you>"
	}
	return fmt.Sprintf("git@github.com:%s/dotfiles.git", user)
}

// PlanInit returns the ordered prerequisites for a working chezmoi + age setup,
// each marked done or with the command to fix it.
func PlanInit(s InitState) []InitStep {
	steps := []InitStep{
		{ID: "chezmoi", Title: "chezmoi installed", Done: s.ChezmoiInstalled},
		{ID: "age-key", Title: "age encryption key configured", Done: s.AgeKeyConfigured},
		{ID: "source", Title: "chezmoi source (private dotfiles repo) initialized", Done: s.SourceInitialized},
	}
	if !s.ChezmoiInstalled {
		steps[0].Command = "brew install chezmoi"
	}
	switch {
	case s.AgeKeyConfigured:
	case s.AgeKeyExists:
		steps[1].Note = "The key is at " + keyPath + ", but chezmoi.toml does not use it yet."
	default:
		steps[1].Command = "age-keygen -o " + keyPath
		steps[1].Note = "Back this key up offline (password manager). Lose it and encrypted files are unrecoverable."
	}
	if !s.SourceInitialized {
		steps[2].Command = "chezmoi init " + RepoURL(s.User)
	}
	return steps
}

// IsReady reports whether every prerequisite is satisfied.
func IsReady(steps []InitStep) bool {
	for _, s := range steps {
		if !s.Done {
			return false
		}
	}
	return true
}

var (
	encryptionKeyRe = regexp.MustCompile(`(?m)^\s*encryption\s*=`)
	ageTableRe      = regexp.MustCompile(`(?m)^\s*\[age\]`)
)

// AgeConfig adds age encryption to a chezmoi.toml (existing may be empty):
// encryption = "age" as the first line, since a top-level key after a
// [table] header would belong to that table, and an [age] table at the end.
// It refuses (ok false) when the file already sets either, so nothing the
// user wrote is changed.
func AgeConfig(existing, identity, recipient string) (string, bool) {
	if encryptionKeyRe.MatchString(existing) || ageTableRe.MatchString(existing) {
		return "", false
	}
	var b strings.Builder
	b.WriteString("encryption = \"age\"\n")
	if existing != "" {
		b.WriteString(existing)
		if !strings.HasSuffix(existing, "\n") {
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "[age]\n    identity = %q\n    recipient = %q\n", identity, recipient)
	return b.String(), true
}
