package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
)

// TestMain lets the test binary re-exec itself as the `dothaven` command — and
// as a fake `chezmoi`, so the destructive --apply path can be driven end-to-end
// without depending on a real chezmoi/age toolchain in CI.
func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"dothaven": main,
		"chezmoi":  fakeChezmoi,
	})
}

// fakeChezmoi stands in for the real chezmoi binary in --apply e2e scripts. It
// answers only the subcommands the export apply path invokes (--version,
// source-path, add). CHEZMOI_SOURCE controls source-path; CHEZMOI_FAIL_ON is a
// substring that makes `add` fail for matching paths (to exercise the
// failure-reporting branch).
func fakeChezmoi() {
	args := os.Args[1:]
	if len(args) == 0 {
		os.Exit(0)
	}
	switch args[0] {
	case "--version":
		fmt.Println("chezmoi version v2.0.0 (fake)")
	case "source-path":
		// `source-path <target>` → that file's .tmpl source; bare → the root.
		if len(args) >= 2 {
			fmt.Println(chezmoiTmplPath(args[len(args)-1]))
		} else {
			fmt.Println(chezmoiSrcRoot())
		}
	case "add":
		target := args[len(args)-1]
		if sub := os.Getenv("CHEZMOI_FAIL_ON"); sub != "" && strings.Contains(target, sub) {
			fmt.Fprintln(os.Stderr, "fake chezmoi: add failed")
			os.Exit(1)
		}
		isTemplate := false
		for _, a := range args[1:] {
			if a == "--template" {
				isTemplate = true
			}
		}
		// Emulate `add --template`: copy the target into the source state as a
		// .tmpl so the export's source-path lookup + rewrite can find it.
		if isTemplate {
			if raw, err := os.ReadFile(target); err == nil {
				dst := chezmoiTmplPath(target)
				_ = os.MkdirAll(filepath.Dir(dst), 0o755)
				_ = os.WriteFile(dst, raw, 0o644)
			}
		}
	}
	os.Exit(0)
}

func chezmoiSrcRoot() string {
	if s := os.Getenv("CHEZMOI_SOURCE"); s != "" {
		return s
	}
	return filepath.Join(os.Getenv("HOME"), ".local", "share", "chezmoi")
}

// chezmoiTmplPath mirrors chezmoi's source naming for a template: ~/.gitconfig
// → <source>/dot_gitconfig.tmpl. Deterministic so add and source-path agree
// across the two separate fake-process invocations.
func chezmoiTmplPath(target string) string {
	base := filepath.Base(target)
	if strings.HasPrefix(base, ".") {
		base = "dot_" + base[1:]
	}
	return filepath.Join(chezmoiSrcRoot(), base+".tmpl")
}

// TestScripts runs every .txtar in testdata/script against the real binary in a
// hermetic temp dir with HOME isolated. Scripts cover the FS/render commands
// (scan, security, compare, list, help/version) — no external tools are invoked.
func TestScripts(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir: "testdata/script",
		Setup: func(e *testscript.Env) error {
			e.Setenv("HOME", e.WorkDir)
			return nil
		},
	})
}
