package chezmoi

import "fmt"

// InitState is the probed readiness of the chezmoi + age prerequisites.
type InitState struct {
	ChezmoiInstalled  bool
	AgeKeyConfigured  bool
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
	if !s.AgeKeyConfigured {
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
