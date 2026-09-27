package cli

import (
	"strings"
	"testing"

	"github.com/doguyilmaz/dothaven/internal/scan"
	"github.com/doguyilmaz/dothaven/internal/snapshot"
)

func items(names ...string) []snapshot.Item {
	out := make([]snapshot.Item, len(names))
	for i, n := range names {
		out[i] = snapshot.Item{Raw: n, Columns: []string{n}}
	}
	return out
}

func TestPlanReinstallOnlyWhatIsMissing(t *testing.T) {
	brewfile := `tap "hashicorp/tap"
brew "git"
brew "hashicorp/tap/terraform"
brew "postgresql@16", restart_service: true
cask "iterm2"
cask "visual-studio-code"
mas "Xcode", id: 497799835
vscode "ms-python.python"
vscode "golang.Go"`
	want := snapshot.Snapshot{
		"apps.brew.bundle":    {Content: &brewfile},
		"packages.npm.global": {Items: items("typescript", "@anthropic-ai/claude-code", scan.Marker)},
		"packages.pipx":       {Items: items("black")},
	}
	have := snapshot.Snapshot{
		"apps.brew.formulae":       {Items: items("git", "terraform", "openssl@3")},
		"apps.brew.casks":          {Items: items("iterm2")},
		"editor.vscode.extensions": {Items: items("golang.go")},
		"packages.npm.global":      {Items: items("typescript")},
	}
	groups := planReinstall(want, have)
	byID := map[string]installGroup{}
	for _, g := range groups {
		byID[g.ID] = g
	}

	if g := byID["brew:brew"]; g.Have != 2 || len(g.Missing) != 1 || !strings.Contains(g.Missing[0], "postgresql@16") {
		t.Errorf("formulae = %+v (tap-qualified terraform counts as installed; the line keeps its options)", g)
	}
	if g := byID["brew:cask"]; g.Have != 1 || len(g.Missing) != 1 {
		t.Errorf("casks = %+v", g)
	}
	if g := byID["brew:vscode"]; g.Have != 1 || len(g.Missing) != 1 || !strings.Contains(g.Missing[0], "ms-python") {
		t.Errorf("vscode (case-insensitive) = %+v", g)
	}
	if g := byID["brew:mas"]; g.Have != -1 || len(g.Missing) != 1 {
		t.Errorf("mas = %+v (unknowable, left to brew bundle)", g)
	}
	if g := byID["packages.npm.global"]; g.Have != 1 || strings.Join(g.Missing, ",") != "@anthropic-ai/claude-code" {
		t.Errorf("npm = %+v (a redacted item is never installed)", g)
	}
	if g := byID["packages.pipx"]; len(g.Missing) != 1 {
		t.Errorf("pipx = %+v", g)
	}

	script, n := renderInstall(groups)
	if n != 6 {
		t.Errorf("renderInstall counted %d, want 6", n)
	}
	for _, want := range []string{`tap "hashicorp/tap"`, `brew "postgresql@16", restart_service: true`, `cask "visual-studio-code"`, "npm install -g @anthropic-ai/claude-code", "pipx install black"} {
		if !strings.Contains(script, want) {
			t.Errorf("script missing %q:\n%s", want, script)
		}
	}
	for _, not := range []string{`brew "git"`, "npm install -g typescript", `cask "iterm2"`} {
		if strings.Contains(script, not) {
			t.Errorf("script reinstalls something already here: %q", not)
		}
	}
}
