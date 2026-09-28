package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// GitHub refuses a path component named .git — a folder, or the .git file a
// submodule or worktree has instead — so a readable push leaves both out.
func TestTreeFilesLeavesOutGitEntries(t *testing.T) {
	root := t.TempDir()
	for p, body := range map[string]string{
		"editor/nvim/init.lua":                       "vim.o.number = true\n",
		"editor/nvim/pack/p/start/plugin/.git":       "gitdir: ../../../../.git/modules/plugin\n",
		"editor/nvim/pack/p/start/plugin/plugin.lua": "return {}\n",
		"extra/.dotfiles/.git/config":                "[core]\n",
		"extra/.dotfiles/zshrc":                      "alias ll=ls\n",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, p), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	files, err := treeFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range files {
		got = append(got, f.Path)
		for _, part := range strings.Split(f.Path, "/") {
			if part == ".git" {
				t.Errorf("%s would be refused by GitHub", f.Path)
			}
		}
	}
	if len(got) != 3 {
		t.Errorf("files = %v, want the three that are not .git", got)
	}
}
