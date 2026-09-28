package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/doguyilmaz/dothaven/internal/chezmoi"
	"github.com/doguyilmaz/dothaven/internal/collect"
	"github.com/doguyilmaz/dothaven/internal/scan"
	"github.com/doguyilmaz/dothaven/internal/snapshot"
	"github.com/doguyilmaz/dothaven/internal/sys"
	"github.com/doguyilmaz/dothaven/internal/tui"
	"github.com/spf13/cobra"
)

const homebrewInstall = `/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"`

func newReinstallCmd(env *sys.OS) *cobra.Command {
	var dryRun, assumeYes bool
	c := &cobra.Command{
		Use:   "reinstall [backup]",
		Short: "Install the apps & packages a backup recorded (only what's missing)",
		Long: "Every backup records what was installed: Homebrew formulae, casks and App Store\n" +
			"apps, global npm/pnpm/bun/pipx/uv/cargo packages, editor extensions, Linux\n" +
			"packages. This compares that list with this machine, shows what is already\n" +
			"installed, and installs the rest: all of it, or the groups and packages you\n" +
			"pick. It runs on the terminal so Homebrew and sudo can ask for your password.\n" +
			"Each step skips itself if its tool is missing, so it is safe to run again.",
		Args:          cobra.MaximumNArgs(1),
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := backupArg(env, args, "Reinstall from which backup?")
			if err != nil || path == "" {
				return ignoreAbort(err)
			}
			want, err := loadSnapshotArg(cmd.Context(), env, path)
			if err != nil {
				return err
			}
			return runReinstallFrom(cmd.Context(), env, want, dryRun, assumeYes)
		},
	}
	c.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be installed and the script, run nothing")
	c.Flags().BoolVar(&assumeYes, "yes", false, "install everything missing without asking (required off a terminal)")
	return c
}

// backupArg resolves the backup a command should read: the argument, or on a
// terminal a pick from the ones on disk.
func backupArg(env *sys.OS, args []string, title string) (string, error) {
	if len(args) == 1 {
		return absBackupArg(args[0])
	}
	if !tui.Interactive() {
		return "", fmt.Errorf("which backup? pass a path to a backup folder or file")
	}
	return pickBackup(env, title)
}

// runReinstall is the restore flow's entry point: dir is an opened backup.
func runReinstall(ctx context.Context, env *sys.OS, dir string, assumeYes bool) error {
	b, err := os.ReadFile(filepath.Join(dir, "inventory", "snapshot.json"))
	if err != nil {
		return fmt.Errorf("this backup has no app & package list (made with --skip inventory, or by an older dothaven)")
	}
	want, err := snapshot.Parse(b)
	if err != nil {
		return err
	}
	return runReinstallFrom(ctx, env, want, false, assumeYes)
}

func runReinstallFrom(ctx context.Context, env *sys.OS, want snapshot.Snapshot, dryRun, assumeYes bool) error {
	var done int64
	stop := startProgress("checking what's installed here", &done, len(installCollectors()))
	have := collect.RunCollectors(collect.Ctx{Context: ctx, Env: env, Home: env.Home(), Done: &done}, installCollectors())
	stop()
	if ctx.Err() != nil {
		return ExitError{Code: 130}
	}

	groups := planReinstall(want, have)
	missing, present := 0, 0
	for _, g := range groups {
		missing += len(g.Missing)
		present += g.Have
	}
	printReinstallPlan(groups)
	if missing == 0 {
		fmt.Println("\n" + good("✓ Everything the backup lists is already installed here."))
		return nil
	}

	chosen := groups
	if dryRun {
		script, _ := renderInstall(chosen)
		fmt.Println("\n" + dim("The script that would run:") + "\n")
		fmt.Println(script)
		return nil
	}
	if tui.Interactive() && !assumeYes {
		var ok bool
		var err error
		chosen, ok, err = chooseInstall(groups, missing)
		if err != nil || !ok {
			fmt.Println("Nothing installed.")
			return ignoreAbort(err)
		}
	} else if err := confirmWrite(os.Stderr, fmt.Sprintf("Install the %d missing packages?", missing), assumeYes); err != nil {
		return err
	}

	script, n := renderInstall(chosen)
	if n == 0 {
		fmt.Println("Nothing selected.")
		return nil
	}
	if runtime.GOOS == "darwin" && strings.Contains(script, "brew bundle") {
		if _, err := exec.LookPath("brew"); err != nil {
			fmt.Println(warn("⚠ Homebrew isn't installed, so the Homebrew part will be skipped."))
			fmt.Printf("  Install it first, then run this again:\n    %s\n\n", kbd(homebrewInstall))
		}
	}
	fmt.Printf("\n%s %s\n\n", bold("Installing"), plural(n, "package"))
	return runScript(ctx, script)
}

// installGroup is one package manager's share of a reinstall.
type installGroup struct {
	ID      string // section id, or "brew:<directive>" for Brewfile lines
	Label   string
	Missing []string // install tokens: names, or whole Brewfile lines
	Have    int      // already installed here; -1 when it cannot be known
	Prelude []string // Brewfile taps a Homebrew group needs installed first
	// Unsafe counts entries that are not a valid package name (or Brewfile
	// line) and would reach a shell; they are left out, and said so.
	Unsafe int
}

// brewLine matches a Brewfile directive and its first quoted argument.
var brewLine = regexp.MustCompile(`^\s*(\w+)\s+"([^"]+)"`)

// sectionGroups are the non-Homebrew managers, in the order they install.
var sectionGroups = []struct{ id, label string }{
	{"packages.node.fnm", "Node versions (fnm)"},
	{"packages.npm.global", "npm global packages"},
	{"packages.pnpm.global", "pnpm global packages"},
	{"packages.bun.global", "bun global packages"},
	{"packages.pipx", "pipx apps"},
	{"packages.uv", "uv tools"},
	{"runtimes.rust.toolchains", "Rust toolchains"},
	{"runtimes.rust.crates", "cargo crates"},
	{"packages.composer", "Composer global packages"},
	{"packages.pub", "Dart global packages"},
	{"packages.dotnet", ".NET tools"},
	{"editor.cursor.extensions", "Cursor extensions"},
	// VS Code extensions normally come with the Brewfile; without Homebrew
	// (Linux) this is how they come back.
	{"editor.vscode.extensions", "VS Code extensions"},
	{"packages.apt", "apt packages"},
	{"packages.dnf", "dnf packages"},
	{"packages.pacman", "pacman packages"},
	{"packages.snap", "snaps"},
	{"packages.flatpak", "flatpaks"},
}

// planReinstall works out, per package manager, what the backup had that this
// machine does not. Pure: want is the backup's inventory, have this machine's.
func planReinstall(want, have snapshot.Snapshot) []installGroup {
	var groups []installGroup
	names := func(id string, fold bool) map[string]bool {
		m := map[string]bool{}
		for _, it := range have[id].Items {
			k := keyOf(it)
			if fold {
				k = strings.ToLower(k)
			}
			m[k] = true
			m[baseName(k)] = true
		}
		return m
	}

	// Homebrew, from the Brewfile: it lists what was installed on request
	// (not every dependency), and brew bundle installs taps and casks with it.
	if c := want["apps.brew.bundle"].Content; c != nil && *c != scan.Marker {
		byDir := map[string]*installGroup{}
		order := []string{}
		var taps []string
		formulae, casks, vscode := names("apps.brew.formulae", false), names("apps.brew.casks", false), names("editor.vscode.extensions", true)
		for _, line := range strings.Split(*c, "\n") {
			m := brewLine.FindStringSubmatch(line)
			if m == nil || strings.Contains(line, scan.Marker) {
				continue
			}
			dir, name := m[1], m[2]
			if dir == "tap" {
				if !chezmoi.SafeBrewLine(line) {
					continue // an unusable tap only means its casks fail to install
				}
				// A cask from a third-party tap is named without it; the tap
				// has to come along with whatever group is installed.
				taps = append(taps, strings.TrimSpace(line))
				continue
			}
			g := byDir[dir]
			if g == nil {
				g = &installGroup{ID: "brew:" + dir, Label: brewLabel(dir), Have: -1}
				if dir == "brew" || dir == "cask" || dir == "vscode" {
					g.Have = 0
				}
				byDir[dir] = g
				order = append(order, dir)
			}
			if !chezmoi.SafeBrewLine(line) {
				g.Unsafe++
				continue
			}
			installed := false
			switch dir {
			case "brew":
				installed = formulae[name] || formulae[baseName(name)]
			case "cask":
				installed = casks[name] || casks[baseName(name)]
			case "vscode":
				installed = vscode[strings.ToLower(name)]
			}
			if installed {
				g.Have++
			} else {
				g.Missing = append(g.Missing, strings.TrimSpace(line))
			}
		}
		for _, d := range order {
			g := *byDir[d]
			g.Prelude = taps
			groups = append(groups, g)
		}
	}

	// The Brewfile brings VS Code extensions only where there is Homebrew;
	// after a move to Linux without it, the extension list has to.
	brewHere := len(have["apps.brew.formulae"].Items) > 0 || len(have["apps.brew.casks"].Items) > 0
	brewVSCode := false
	for _, g := range groups {
		brewVSCode = brewVSCode || (g.ID == "brew:vscode" && brewHere)
	}
	for _, sg := range sectionGroups {
		sec, ok := want[sg.id]
		if !ok || len(sec.Items) == 0 || (sg.id == "editor.vscode.extensions" && brewVSCode) {
			continue
		}
		fold := strings.HasSuffix(sg.id, ".extensions")
		present := names(sg.id, fold)
		g := installGroup{ID: sg.id, Label: sg.label}
		for _, it := range sec.Items {
			if it.Raw == scan.Marker {
				continue
			}
			k := keyOf(it)
			if !chezmoi.SafeName(k) {
				g.Unsafe++
				continue
			}
			if fold {
				k = strings.ToLower(k)
			}
			if present[k] {
				g.Have++
			} else {
				g.Missing = append(g.Missing, keyOf(it))
			}
		}
		groups = append(groups, g)
	}
	return groups
}

func baseName(s string) string {
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		return s[i+1:]
	}
	return s
}

func brewLabel(directive string) string {
	switch directive {
	case "brew":
		return "Homebrew formulae"
	case "cask":
		return "Homebrew apps (casks)"
	case "mas":
		return "Mac App Store apps"
	case "vscode":
		return "VS Code extensions"
	}
	return "Homebrew: " + directive
}

// renderInstall turns the chosen groups into the install script, reusing the
// generator the chezmoi export uses. It returns the script and how many
// packages it installs.
func renderInstall(groups []installGroup) (string, int) {
	var m chezmoi.Manifest
	var brewfile, taps []string
	n := 0
	for _, g := range groups {
		n += len(g.Missing)
		if strings.HasPrefix(g.ID, "brew:") {
			if len(g.Missing) > 0 && taps == nil {
				taps = g.Prelude
			}
			brewfile = append(brewfile, g.Missing...)
			continue
		}
		switch g.ID {
		case "packages.node.fnm":
			m.NodeVersions = g.Missing
		case "packages.npm.global":
			m.NpmGlobals = g.Missing
		case "packages.pnpm.global":
			m.PnpmGlobals = g.Missing
		case "packages.bun.global":
			m.BunGlobals = g.Missing
		case "packages.pipx":
			m.PipxPackages = g.Missing
		case "packages.uv":
			m.UvTools = g.Missing
		case "runtimes.rust.toolchains":
			m.RustToolchains = g.Missing
		case "runtimes.rust.crates":
			m.CargoCrates = g.Missing
		case "packages.composer":
			m.ComposerGlobals = g.Missing
		case "packages.pub":
			m.PubGlobals = g.Missing
		case "packages.dotnet":
			m.DotnetTools = g.Missing
		case "editor.cursor.extensions":
			m.CursorExtensions = g.Missing
		case "editor.vscode.extensions":
			m.VSCodeExtensions = g.Missing
		case "packages.apt":
			m.AptPackages = g.Missing
		case "packages.dnf":
			m.DnfPackages = g.Missing
		case "packages.pacman":
			m.PacmanPackages = g.Missing
		case "packages.snap":
			m.SnapPackages = g.Missing
		case "packages.flatpak":
			m.FlatpakPackages = g.Missing
		}
	}
	if len(brewfile) > 0 {
		m.Brewfile = strings.Join(append(append([]string(nil), taps...), brewfile...), "\n")
	}
	script, _ := chezmoi.BuildPackageInstallScript(m)
	return script, n
}

func printReinstallPlan(groups []installGroup) {
	fmt.Println()
	for _, g := range groups {
		have := ""
		switch {
		case g.Have > 0:
			have = good(fmt.Sprintf("✓ %d installed", g.Have))
		case g.Have < 0:
			have = dim("(Homebrew checks these itself)")
		}
		// Padded before colouring: escape codes would count as width.
		todo := dim(padTo("nothing missing", 18))
		if len(g.Missing) > 0 {
			todo = warn(padTo(fmt.Sprintf("%d to install", len(g.Missing)), 18))
		}
		fmt.Printf("  %s %s  %s\n", padTo(g.Label, 28), todo, have)
		if g.Unsafe > 0 {
			fmt.Printf("    %s\n", warn(fmt.Sprintf("⚠ %s left out: not a valid package name, and it would reach a shell", plural(g.Unsafe, "entry"))))
		}
	}
}

// chooseInstall asks what to install: everything missing, some groups, or
// some packages.
func chooseInstall(groups []installGroup, missing int) ([]installGroup, bool, error) {
	c, err := tui.Ask("What should be installed?", "Already-installed packages are left alone.", []tui.Choice{
		{Label: fmt.Sprintf("Everything missing (%d)", missing), Value: "all"},
		{Label: "Choose groups", Value: "groups", Hint: "e.g. only Homebrew apps and npm"},
		{Label: "Choose packages", Value: "pkgs", Hint: "type / to filter"},
		{Label: "Cancel", Value: "cancel"},
	})
	if err != nil {
		return nil, false, err
	}
	switch c {
	case "all":
		return groups, true, nil
	case "groups":
		var items []tui.PickItem
		for _, g := range groups {
			if len(g.Missing) > 0 {
				items = append(items, tui.PickItem{Label: g.Label, Value: g.ID, Hint: plural(len(g.Missing), "package"), Selected: true})
			}
		}
		picked, err := tui.MultiPick("Which groups?", "space toggles · enter installs", items)
		if err != nil || len(picked) == 0 {
			return nil, false, err
		}
		var out []installGroup
		for _, g := range groups {
			if contains(picked, g.ID) {
				out = append(out, g)
			}
		}
		return out, true, nil
	case "pkgs":
		var items []tui.PickItem
		for _, g := range groups {
			for _, p := range g.Missing {
				label := p
				if m := brewLine.FindStringSubmatch(p); m != nil {
					label = m[2]
				}
				items = append(items, tui.PickItem{Label: label, Value: g.ID + "\x00" + p, Hint: g.Label, Selected: true})
			}
		}
		sort.SliceStable(items, func(i, j int) bool { return items[i].Hint < items[j].Hint })
		picked, err := tui.MultiPick("Which packages?", "space toggles · / filters · enter installs", items)
		if err != nil || len(picked) == 0 {
			return nil, false, err
		}
		sel := map[string]bool{}
		for _, p := range picked {
			sel[p] = true
		}
		var out []installGroup
		for _, g := range groups {
			ng := g
			ng.Missing = nil
			for _, p := range g.Missing {
				if sel[g.ID+"\x00"+p] {
					ng.Missing = append(ng.Missing, p)
				}
			}
			if len(ng.Missing) > 0 {
				out = append(out, ng)
			}
		}
		return out, true, nil
	}
	return nil, false, nil
}

// runScript runs an install script attached to this terminal.
//
// No timeout, deliberately: `brew bundle` on a fresh machine is tens of
// minutes of downloads, and a kill at some arbitrary deadline would leave it
// half-installed. Ctrl-C still stops it: the context kills the script. The
// script is written to a private temporary directory and removed afterwards.
func runScript(ctx context.Context, script string) error {
	tmp, done, err := sys.PrivateTempDir("install")
	if err != nil {
		return err
	}
	defer done()
	path := filepath.Join(tmp, "install.sh")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "bash", path)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ExitError{Code: 130}
		}
		return fmt.Errorf("install script failed: %w", err)
	}
	fmt.Println("\n" + good("✓ Done.") + " Anything a tool could not install was skipped; run this again to see what is still missing.")
	return nil
}
