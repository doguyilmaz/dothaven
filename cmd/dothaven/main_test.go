package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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
		"defaults": fakeDefaults,
		"brew":     fakeBrew,
	})
}

// fakeBrew stands in for Homebrew so the services export/import round-trip is
// testable on any OS. `brew --prefix` echoes $BREW_PREFIX (default /opt/homebrew).
func fakeBrew() {
	if args := os.Args[1:]; len(args) >= 1 && args[0] == "--prefix" {
		if p := os.Getenv("BREW_PREFIX"); p != "" {
			fmt.Println(p)
		} else {
			fmt.Println("/opt/homebrew")
		}
	}
	os.Exit(0)
}

// fakeDefaults stands in for the macOS `defaults` tool so the defaults
// export/import round-trip can be tested on any CI OS. Only com.googlecode.iterm2
// reports keys; every other domain exports an empty dict (skipped on export).
func fakeDefaults() {
	args := os.Args[1:]
	if len(args) == 0 {
		os.Exit(0)
	}
	if len(args) < 2 && args[0] != "domains" {
		os.Exit(0)
	}
	switch args[0] {
	case "domains":
		fmt.Println("com.googlecode.iterm2, com.apple.Terminal")
	case "write":
		fmt.Println("wrote")
	case "export":
		switch args[1] {
		case "com.googlecode.iterm2":
			fmt.Println(`<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>Theme</key><string>Dark</string></dict></plist>`)
		// A core domain, so the per-key pass has something it would apply by
		// default — iTerm2's is held back as application state.
		case "NSGlobalDomain":
			fmt.Println(`<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>com.apple.swipescrolldirection</key><false/></dict></plist>`)
		default:
			fmt.Println(`<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict/></plist>`)
		}
	case "import":
		fmt.Println("imported")
	}
	os.Exit(0)
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
	case "apply":
		// no-op success; the real one would write $HOME and run scripts.
		fmt.Println("applied")
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
		Cmds: map[string]func(*testscript.TestScript, bool, []string){
			// filemode asserts a file's permission bits. `stat` spells this
			// differently on macOS and Linux, and the assertion has to hold on
			// both — a permission test that only runs in CI is not a
			// permission test.
			// globpath resolves a single-match glob into an env var. Backups
			// are named for the host and the minute they were taken, so a
			// script cannot spell the path it just created.
			"globpath": func(ts *testscript.TestScript, neg bool, args []string) {
				if len(args) != 2 {
					ts.Fatalf("usage: globpath <ENVVAR> <glob>")
				}
				matches, err := filepath.Glob(ts.MkAbs(args[1]))
				if err != nil {
					ts.Fatalf("bad glob %q: %v", args[1], err)
				}
				if len(matches) != 1 {
					ts.Fatalf("glob %q matched %d files, want exactly 1: %v", args[1], len(matches), matches)
				}
				ts.Setenv(args[0], matches[0])
			},
			"filemode": func(ts *testscript.TestScript, neg bool, args []string) {
				if len(args) != 2 {
					ts.Fatalf("usage: filemode <octal> <file>")
				}
				want, err := strconv.ParseUint(args[0], 8, 32)
				if err != nil {
					ts.Fatalf("bad mode %q: %v", args[0], err)
				}
				fi, err := os.Stat(ts.MkAbs(args[1]))
				if err != nil {
					ts.Fatalf("%v", err)
				}
				got := fi.Mode().Perm()
				switch {
				case neg && got == os.FileMode(want):
					ts.Fatalf("%s is %04o, expected it not to be", args[1], got)
				case !neg && got != os.FileMode(want):
					ts.Fatalf("%s is %04o, want %04o", args[1], got, want)
				}
			},
		},
	})
}
