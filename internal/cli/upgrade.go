package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/doguyilmaz/dothaven/internal/release"
	"github.com/doguyilmaz/dothaven/internal/sys"
	"github.com/spf13/cobra"
)

const (
	// updateCheckTTL is how long an answer is trusted. Once a day is enough to
	// hear about a release the same week and rare enough that nobody's network
	// notices.
	updateCheckTTL = 24 * time.Hour
	// updateGrace is how long the process waits at exit for a check that is
	// still in flight. It matches the fetch timeout, so the wait can never
	// outlast the request it is waiting for.
	updateGrace = 2 * time.Second
	// noUpdateEnv silences the check entirely. Presence is what counts, not the
	// value — the same rule NO_COLOR follows, and the one style.go already uses.
	noUpdateEnv = "DOTHAVEN_NO_UPDATE_CHECK"

	upgradeCmdName = "upgrade"
)

// suppressNotice reports whether the command about to run is one that must not
// be followed by the update notice.
//
// Only `upgrade` qualifies. Ending it with "an upgrade is available" reads as a
// failure — most of all right after one succeeded, because the process printing
// the line is still the old binary and still reports the old version. Resolving
// through cobra rather than matching os.Args means the `update` alias is
// covered without naming it twice.
func suppressNotice(root *cobra.Command, args []string) bool {
	target, _, err := root.Find(args)
	return err == nil && target != nil && target.Name() == upgradeCmdName
}

// newUpgradeCmd updates dothaven by asking whoever installed it to do the work.
//
// dothaven deliberately never writes over its own binary. Homebrew records
// which version it put in the Caskroom; a binary that replaces itself leaves
// that record describing a file that is no longer there, `brew outdated` keeps
// reporting the old version forever, and the next real `brew upgrade` throws
// the replacement away. Delegating also means the download is verified against
// the cask's pinned checksum by the tool that pinned it, rather than by
// verification code written here.
func newUpgradeCmd(env *sys.OS, version string) *cobra.Command {
	var checkOnly, assumeYes bool
	c := &cobra.Command{
		Use:     upgradeCmdName,
		Aliases: []string{"update"},
		Short:   "Update dothaven to the latest release",
		Long: "Checks GitHub for the newest release, works out how this copy of dothaven was\n" +
			"installed, and runs that installer's upgrade for you.\n\n" +
			"dothaven never overwrites its own binary. Homebrew tracks the version it\n" +
			"installed, so replacing that file behind its back leaves `brew outdated`\n" +
			"describing something that no longer exists — and the next `brew upgrade`\n" +
			"would undo it anyway.\n\n" +
			"Use --check to see what is available without changing anything.",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			checker := newUpdateChecker(env, version)
			latest, err := checker.Latest(ctx, true)
			if err != nil {
				return fmt.Errorf("could not check for updates: %w", err)
			}
			shown := strings.TrimPrefix(latest, "v")

			// A `go build` binary has no version to compare against.
			if !release.Comparable(version) {
				fmt.Printf("This is a development build, so there is no version to compare.\n")
				fmt.Printf("The latest release is %s.\n\n", bold(shown))
				fmt.Printf("  %s\n", kbd(release.Command(release.GoInstall)))
				return nil
			}

			if !release.Newer(version, latest) {
				fmt.Printf("%s dothaven %s is the latest release.\n", good("✓"), bold(version))
				return nil
			}
			fmt.Printf("%s dothaven %s → %s\n", warn("⇡"), version, warn(shown))

			method, path := detectInstall(ctx, env)
			steps := release.Steps(method)
			if len(steps) == 0 {
				owned := "this binary"
				if path != "" {
					owned = dim(shortenPath(path, 48))
				}
				fmt.Printf("\nNo package manager owns %s, so it has to be replaced by hand:\n", owned)
				fmt.Printf("  %s\n", kbd(release.ReleasesURL))
				return nil
			}

			fmt.Printf("\n%s\n", installerNote(method))
			fmt.Printf("  %s\n", kbd(release.Command(method)))
			if checkOnly {
				return nil
			}

			fmt.Println()
			if err := confirmWrite(os.Stderr, "Run it now?", assumeYes); err != nil {
				return err
			}
			for _, step := range steps {
				line := strings.Join(step.Args, " ")
				fmt.Printf("\n%s\n", dim("$ "+line))
				err := runStreaming(ctx, step.Args[0], step.Args[1:]...)
				if err == nil {
					continue
				}
				if !step.Optional {
					return fmt.Errorf("%s failed: %w", step.Args[0], err)
				}
				fmt.Printf("\n%s %s failed, carrying on — it may have nothing to do with dothaven.\n",
					warn("⚠"), kbd(line))
			}
			fmt.Printf("\n%s Done — run %s to confirm.\n", good("✓"), kbd("dothaven --version"))
			return nil
		},
	}
	c.Flags().BoolVar(&checkOnly, "check", false, "report what is available, change nothing")
	c.Flags().BoolVar(&assumeYes, "yes", false, "skip the confirmation prompt")
	return c
}

func installerNote(m release.Method) string {
	switch m {
	case release.Homebrew:
		return "Homebrew installed this, so Homebrew replaces it:"
	case release.GoInstall:
		return "This was built with `go install`, so rebuild it the same way:"
	}
	return ""
}

// detectInstall resolves the running binary and classifies who owns it,
// returning the resolved path so a manual install can be named in the output.
func detectInstall(ctx context.Context, env *sys.OS) (release.Method, string) {
	exe, err := os.Executable()
	if err != nil {
		return release.Manual, ""
	}
	// /opt/homebrew/bin/dothaven is a symlink into the versioned Caskroom
	// directory; the link is what is on PATH, the target is what identifies it.
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	// brewPrefix lives in services.go — the same question, already answered once.
	return release.Detect(exe, brewPrefix(ctx), goBinDir(os.Getenv, env.Home())), exe
}

// goBinDir is where `go install` would have put this binary.
func goBinDir(getenv func(string) string, home string) string {
	if b := getenv("GOBIN"); b != "" {
		return b
	}
	if p := getenv("GOPATH"); p != "" {
		// GOPATH is a list and `go install` writes to the first entry.
		first, _, _ := strings.Cut(p, string(os.PathListSeparator))
		return filepath.Join(first, "bin")
	}
	return filepath.Join(home, "go", "bin")
}

// runStreaming runs a command with the terminal attached, so brew's progress is
// visible as it happens rather than arriving in one block at the end.
//
// Deliberately without a timeout, unlike everything else that shells out here:
// `brew update` against a cold tap regularly runs for minutes, this only ever
// happens because someone asked for it and is watching, and Ctrl-C still works
// through the context.
func runStreaming(ctx context.Context, name string, args ...string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func newUpdateChecker(env *sys.OS, version string) *release.Checker {
	return &release.Checker{
		Fetcher:   release.HTTP("dothaven/" + version),
		CachePath: filepath.Join(env.CacheDir(), "update-check.json"),
		TTL:       updateCheckTTL,
	}
}

// --- the passive notice ---

// updateCheckAllowed decides whether this run may look for a newer version.
//
// The terminal test is the load-bearing one. dothaven's stdout is parsed —
// snapshots are deterministic JSON — so nothing may be added to it, and a run
// whose stderr is not a terminal either is being captured or is a CI job, where
// a banner is noise nobody will read.
func updateCheckAllowed(version string, optedOut, inCI, stderrTTY bool) bool {
	return !optedOut && !inCI && stderrTTY && release.Comparable(version)
}

// updateNotice writes the one-line "there is a newer version" banner.
//
// One line, on stderr. A boxed multi-line banner after every command is the
// thing people turn off, and a notice nobody sees tells nobody anything.
func updateNotice(w io.Writer, current, latest string) {
	fmt.Fprintf(w, "%s dothaven %s is available (you have %s) — run %s\n",
		warn("⇡"), warn(strings.TrimPrefix(latest, "v")), current, kbd("dothaven upgrade"))
}

// updateProbe carries an update check that runs alongside the command.
type updateProbe struct {
	checker *release.Checker
	done    <-chan struct{}
}

// startUpdateCheck begins a background check, or returns nil when this run is
// not one that may check.
func startUpdateCheck(ctx context.Context, env *sys.OS, version string) *updateProbe {
	_, optedOut := os.LookupEnv(noUpdateEnv)
	_, inCI := os.LookupEnv("CI")
	if !updateCheckAllowed(version, optedOut, inCI, stderrIsTTY()) {
		return nil
	}
	checker := newUpdateChecker(env, version)
	done := make(chan struct{})
	go func() {
		defer close(done)
		// A version check must never be why a command failed, so the result is
		// dropped here: what it is for is leaving a fresher answer in the
		// cache, which is the only thing finish reads.
		_, _ = checker.Latest(ctx, false)
	}()
	return &updateProbe{checker: checker, done: done}
}

// finish prints the notice, if there is one, once the command has finished.
//
// The answer always comes from the cache, never from the in-flight request, so
// what gets printed does not depend on winning a race. Waiting a moment for the
// request first is only so today's answer can be today's, and a check that does
// not land in time simply leaves its answer for the next run.
func (p *updateProbe) finish(w io.Writer, version string) {
	if p == nil {
		return
	}
	select {
	case <-p.done:
	case <-time.After(updateGrace):
	}
	if latest := p.checker.Cached(); release.Newer(version, latest) {
		updateNotice(w, version, latest)
	}
}
