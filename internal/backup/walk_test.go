package backup

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/doguyilmaz/dothaven/internal/registry"
)

// Folders a package manager or build recreates are left out wherever they
// are, and said so; build output only where the manifest that rebuilds it is
// beside it, and a JavaScript project's only with its node_modules there.
func TestWalkLeavesOutRebuildable(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{
		"settings.json",
		"node_modules/a/index.js",
		"__pycache__/x.pyc",
		"skill/package.json", "skill/index.js", "skill/node_modules/b/index.js", "skill/dist/index.js",
		"shipped/package.json", "shipped/dist/index.js",
		"py/pyproject.toml", "py/dist/pkg.whl", "py/.venv/bin/python",
		"env/pyvenv.cfg", "env/lib/site.py",
		"rust/Cargo.toml", "rust/src/main.rs", "rust/target/debug/app",
		"notes/dist/readme.md",
		"app/Code Cache/js/1", "app/User/settings.json",
	} {
		mustWrite(t, filepath.Join(root, f), "x")
	}
	files, skipped := Walk(registry.BackupTarget{Src: root, Dest: "ai/x", IsDir: true}, WalkOptions{})
	var got []string
	for _, f := range files {
		got = append(got, strings.TrimPrefix(f.Dest, "ai/x/"))
	}
	want := []string{"app/User/settings.json", "notes/dist/readme.md", "py/pyproject.toml", "rust/Cargo.toml", "rust/src/main.rs",
		"settings.json", "shipped/dist/index.js", "shipped/package.json", "skill/index.js", "skill/package.json"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("carried:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	var left []string
	for _, s := range skipped {
		if !IsRebuildable(s) {
			t.Errorf("%s skipped as %q", s.Dest, s.Reason)
		}
		left = append(left, strings.TrimPrefix(s.Dest, "ai/x/")+" "+RebuildKind(s))
	}
	sort.Strings(left)
	wantLeft := []string{"__pycache__ cache", "app/Code Cache cache", "env virtual environment", "node_modules dependencies",
		"py/.venv virtual environment", "py/dist build output", "rust/target build output",
		"skill/dist build output", "skill/node_modules dependencies"}
	if strings.Join(left, "\n") != strings.Join(wantLeft, "\n") {
		t.Errorf("left out:\n%s\nwant:\n%s", strings.Join(left, "\n"), strings.Join(wantLeft, "\n"))
	}

	// A folder named on purpose is carried, whatever it is called.
	files, _ = Walk(registry.BackupTarget{Src: filepath.Join(root, "node_modules"), Dest: "extra/nm", IsDir: true}, WalkOptions{})
	if len(files) != 1 {
		t.Errorf("an include of node_modules itself carried %d files", len(files))
	}
}
