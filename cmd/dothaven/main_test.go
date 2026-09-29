package main

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/doguyilmaz/dothaven/internal/github/githubtest"
	"github.com/rogpeppe/go-internal/testscript"
)

// TestMain lets the test binary re-exec itself as the `dothaven` command and
// as a fake `chezmoi`, so the destructive --apply path can be driven end-to-end
// without depending on a real chezmoi/age toolchain in CI.
func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"dothaven": main,
		"chezmoi":  noLeak(fakeChezmoi),
		"defaults": noLeak(fakeDefaults),
		"brew":     noLeak(fakeBrew),
		// A test must never restart the real Dock of the Mac it runs on.
		"killall": func() { os.Exit(0) },
	})
}

// noLeak wraps a fake tool: dothaven must not hand its passphrase or token to
// the processes it starts. A fake that sees one records it in $LEAK_LOG,
// which a script asserts does not exist.
func noLeak(f func()) func() {
	return func() {
		for _, name := range []string{"DOTHAVEN_PASSPHRASE", "DOTHAVEN_GITHUB_TOKEN"} {
			if _, ok := os.LookupEnv(name); ok {
				if log := os.Getenv("LEAK_LOG"); log != "" {
					fh, _ := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
					fmt.Fprintf(fh, "%s saw %s\n", filepath.Base(os.Args[0]), name)
					fh.Close()
				}
			}
		}
		f()
	}
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
	case "read":
		if len(args) >= 3 && args[1] == "com.apple.dock" && args[2] == "persistent-apps" {
			fmt.Println(`( { "tile-data" = { "file-data" = { "_CFURLString" = "file://` + os.Getenv("DOCK_APP") + `/"; }; }; } )`)
		}
	case "write", "delete":
		if state := os.Getenv("FAKE_NOKEY"); state != "" && args[0] == "write" && len(args) >= 3 {
			f, _ := os.OpenFile(state, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			fmt.Fprintf(f, "%s %s\n", args[1], args[2])
			f.Close()
		}
		// DEFAULTS_LOG records what would have changed on a real Mac.
		if log := os.Getenv("DEFAULTS_LOG"); log != "" {
			f, _ := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			fmt.Fprintln(f, strings.Join(args, " "))
			f.Close()
		}
		fmt.Println("wrote")
	case "read-type":
		// FAKE_NOKEY names a state file: a key is set on this Mac only once
		// a write has recorded it there.
		if state := os.Getenv("FAKE_NOKEY"); state != "" && len(args) >= 3 {
			b, _ := os.ReadFile(state)
			if !strings.Contains(string(b), args[1]+" "+args[2]+"\n") {
				fmt.Fprintln(os.Stderr, "The domain/default pair does not exist")
				os.Exit(1)
			}
		}
		// FAKE_READTYPE simulates `defaults` storing a value as a string.
		if t := os.Getenv("FAKE_READTYPE"); t != "" {
			fmt.Println("Type is " + t)
		} else {
			fmt.Println("Type is array")
		}
	case "export":
		switch args[1] {
		case "com.googlecode.iterm2":
			fmt.Println(`<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>Theme</key><string>Dark</string></dict></plist>`)
		// A core domain, so the per-key pass has something it would apply by
		// default (iTerm2's is held back as application state).
		case "NSGlobalDomain":
			// FAKE_SCROLL flips the live value, so an import can be tested
			// both against a Mac that already has the setting and one that
			// does not.
			v := "<false/>"
			if os.Getenv("FAKE_SCROLL") == "true" {
				v = "<true/>"
			}
			// FAKE_LANG adds the language order, a nested value.
			lang := ""
			if l := os.Getenv("FAKE_LANG"); l != "" {
				lang = "<key>AppleLanguages</key>\n\t<array>\n\t\t<string>" + l + "</string>\n\t</array>"
			}
			fmt.Println(`<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>com.apple.swipescrolldirection</key>` + v + lang + `</dict></plist>`)
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
		// Like the real one, `add` takes any number of targets after its flags.
		isTemplate := false
		var targets []string
		for _, a := range args[1:] {
			switch {
			case a == "--template":
				isTemplate = true
			case strings.HasPrefix(a, "-"):
			default:
				targets = append(targets, a)
			}
		}
		for _, target := range targets {
			if sub := os.Getenv("CHEZMOI_FAIL_ON"); sub != "" && strings.Contains(target, sub) {
				fmt.Fprintln(os.Stderr, "fake chezmoi: add failed")
				os.Exit(1)
			}
		}
		for _, target := range targets {
			// Emulate `add --template`: copy the target into the source state
			// as a .tmpl so the export's source-path lookup + rewrite can find it.
			if isTemplate {
				if raw, err := os.ReadFile(target); err == nil {
					dst := chezmoiTmplPath(target)
					_ = os.MkdirAll(filepath.Dir(dst), 0o755)
					_ = os.WriteFile(dst, raw, 0o644)
				}
			}
			if log := os.Getenv("CHEZMOI_LOG"); log != "" {
				f, _ := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
				fmt.Fprintf(f, "%s %s\n", strings.Join(args[:len(args)-len(targets)], " "), target)
				f.Close()
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
// (scan, security, compare, list, help/version), and no external tools are
// invoked.
func TestScripts(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir: "testdata/script",
		Setup: func(e *testscript.Env) error {
			e.Setenv("HOME", e.WorkDir)
			// A fake GitHub per script, and the credential store on disk:
			// no test may reach github.com or the real keychain.
			gh := githubtest.New("test-token", "tester")
			gh.AddRepo("tester/public-one", false)
			gh.AppSlug = "dothaven"
			e.Values["github"] = gh
			e.Defer(gh.Close)
			e.Setenv("DOTHAVEN_GITHUB_API", gh.URL)
			e.Setenv("DOTHAVEN_GITHUB_WEB", gh.URL)
			e.Setenv("DOTHAVEN_SECRET_STORE", "file")
			e.Setenv("DOTHAVEN_NO_UPDATE_CHECK", "1")
			// A real gh login on the test machine must not leak in.
			e.Setenv("GH_CONFIG_DIR", e.WorkDir+"/.gh-none")
			e.Setenv("GH_TOKEN", "")
			e.Setenv("GITHUB_TOKEN", "")
			// Only the script's own git config: the machine's could sign.
			e.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			e.Setenv("GIT_CONFIG_GLOBAL", e.WorkDir+"/.gitconfig")
			return nil
		},
		Cmds: map[string]func(*testscript.TestScript, bool, []string){
			// signedby asserts the newest commit on the fake GitHub names this
			// author and committer ("-" for one left to GitHub), and with a
			// third argument "signed", that it came signed.
			"signedby": func(ts *testscript.TestScript, neg bool, args []string) {
				if len(args) != 2 && (len(args) != 3 || args[2] != "signed") {
					ts.Fatalf("usage: signedby <author> <committer> [signed]")
				}
				sigs := ts.Value("github").(*githubtest.Server).Signatures()
				if len(sigs) == 0 {
					ts.Fatalf("no commits yet")
				}
				last := sigs[len(sigs)-1]
				want := func(s string) string {
					if s == "-" {
						return ""
					}
					return s
				}
				ok := last.Author == want(args[0]) && last.Committer == want(args[1]) && last.Signed == (len(args) == 3)
				if ok == neg {
					ts.Fatalf("newest commit: author %q, committer %q, signed %v", last.Author, last.Committer, last.Signed)
				}
			},
			// randfile writes a file of n MiB that does not compress, for a
			// backup too large for one GitHub file.
			"randfile": func(ts *testscript.TestScript, neg bool, args []string) {
				if len(args) != 2 {
					ts.Fatalf("usage: randfile <path> <MiB>")
				}
				n, err := strconv.Atoi(args[1])
				if err != nil {
					ts.Fatalf("bad size %q", args[1])
				}
				p := ts.MkAbs(args[0])
				ts.Check(os.MkdirAll(filepath.Dir(p), 0o755))
				b := make([]byte, n<<20)
				_, _ = rand.NewChaCha8([32]byte{1}).Read(b)
				ts.Check(os.WriteFile(p, b, 0o644))
			},
			// ghunverified makes the fake GitHub refuse to verify signatures,
			// giving this reason.
			"ghunverified": func(ts *testscript.TestScript, neg bool, args []string) {
				if len(args) != 1 {
					ts.Fatalf("usage: ghunverified <reason>")
				}
				ts.Value("github").(*githubtest.Server).Unverified = args[0]
			},
			// filemode asserts a file's permission bits. `stat` spells this
			// differently on macOS and Linux, and the assertion has to hold on
			// both, not only on the CI runner.
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
