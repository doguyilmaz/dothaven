package cli

import (
	"reflect"
	"testing"

	"github.com/doguyilmaz/dothaven/internal/snapshot"
)

func TestRemediationCommand(t *testing.T) {
	cases := map[string]string{
		"apps.brew.formulae":       "brew install",
		"packages.npm.global":      "npm install -g",
		"packages.pipx":            "pipx install",
		"editor.cursor.extensions": "cursor --install-extension",
		"runtimes.rust.toolchains": "rustup toolchain install",
		"shell.zshrc":              "", // no single reinstall command
		"runtimes.go":              "", // go bins aren't reliably reinstallable
	}
	for id, want := range cases {
		if got := remediationCommand(id); got != want {
			t.Errorf("remediationCommand(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestFirstToken(t *testing.T) {
	if got := firstToken("typescript 5.4.0"); got != "typescript" {
		t.Errorf("firstToken = %q, want typescript", got)
	}
	if got := firstToken("anthropic.claude-code"); got != "anthropic.claude-code" {
		t.Errorf("firstToken should pass through a token with no space: %q", got)
	}
}

func TestIsInstallable(t *testing.T) {
	yes := []string{"packages.npm.global", "runtimes.go", "apps.brew.formulae", "apps.macos", "fonts.user", "editor.vscode.extensions"}
	no := []string{"meta", "shell.zshrc", "apps.raycast", "ssh.hosts", "home.dotfiles.review"}
	for _, id := range yes {
		if !isInstallable(id) {
			t.Errorf("isInstallable(%q) = false, want true", id)
		}
	}
	for _, id := range no {
		if isInstallable(id) {
			t.Errorf("isInstallable(%q) = true, want false", id)
		}
	}
}

func TestKeyOf(t *testing.T) {
	if got := keyOf(snapshot.Item{Raw: "pkg@1.2.3", Columns: []string{"pkg", "1.2.3"}}); got != "pkg" {
		t.Errorf("keyOf with columns = %q, want pkg", got)
	}
	if got := keyOf(snapshot.Item{Raw: "lonely"}); got != "lonely" {
		t.Errorf("keyOf without columns = %q, want lonely", got)
	}
}

func item(name, version string) snapshot.Item {
	if version == "" {
		return snapshot.Item{Raw: name, Columns: []string{name}}
	}
	return snapshot.Item{Raw: name + "@" + version, Columns: []string{name, version}}
}

func TestFindMissing(t *testing.T) {
	want := snapshot.Snapshot{
		"packages.npm.global": {Items: []snapshot.Item{item("typescript", "5.4.0"), item("eslint", "9.0.0")}},
		// Version drift only → not missing (keyed on name).
		"runtimes.go": {Items: []snapshot.Item{item("go", "1.26.3")}},
		// Non-installable section → ignored entirely.
		"shell.zshrc": {Items: []snapshot.Item{item("anything", "")}},
	}
	have := snapshot.Snapshot{
		"packages.npm.global": {Items: []snapshot.Item{item("typescript", "5.4.0")}}, // eslint absent
		"runtimes.go":         {Items: []snapshot.Item{item("go", "1.27.0")}},        // different version, same name
	}

	got := findMissing(want, have)
	exp := map[string][]string{"packages.npm.global": {"eslint@9.0.0"}}
	if !reflect.DeepEqual(got, exp) {
		t.Errorf("findMissing = %v, want %v", got, exp)
	}
}

func TestFindMissingFullParity(t *testing.T) {
	snap := snapshot.Snapshot{"fonts.user": {Items: []snapshot.Item{item("FiraCode", "")}}}
	if got := findMissing(snap, snap); len(got) != 0 {
		t.Errorf("identical snapshots should yield no missing, got %v", got)
	}
}
