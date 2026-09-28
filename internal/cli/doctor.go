package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/doguyilmaz/dothaven/internal/snapshot"
	"github.com/doguyilmaz/dothaven/internal/sys"
	"github.com/spf13/cobra"
)

// isInstallable reports whether a section is installable inventory worth a parity
// check — things you can reinstall on a fresh machine.
func isInstallable(id string) bool {
	return strings.HasPrefix(id, "packages.") ||
		strings.HasPrefix(id, "runtimes.") ||
		strings.HasPrefix(id, "vm.") ||
		strings.HasPrefix(id, "apps.brew.") ||
		id == "apps.macos" ||
		strings.HasPrefix(id, "fonts.") ||
		strings.HasSuffix(id, ".extensions")
}

// keyOf is an item's parity identity: columns[0] (the name) falling back to raw.
// Keying on name ignores version drift — parity asks "present?", not "same version?".
func keyOf(it snapshot.Item) string {
	if len(it.Columns) > 0 {
		return it.Columns[0]
	}
	return it.Raw
}

// findMissing returns, per installable section, items present in want but absent
// from have (the live machine), reported by their raw form.
func findMissing(want, have snapshot.Snapshot) map[string][]string {
	missing := map[string][]string{}
	for id, sec := range want {
		if !isInstallable(id) || len(sec.Items) == 0 {
			continue
		}
		present := make(map[string]bool)
		for _, it := range have[id].Items {
			present[keyOf(it)] = true
		}
		var gone []string
		for _, it := range sec.Items {
			if !present[keyOf(it)] {
				gone = append(gone, it.Raw)
			}
		}
		if len(gone) > 0 {
			missing[id] = gone
		}
	}
	return missing
}

// remediationCommand maps a snapshot section id to the command that reinstalls
// its items, so doctor reports how to fix drift, not just what's missing.
// Empty when there's no single obvious reinstall command (the user decides).
func remediationCommand(id string) string {
	switch id {
	case "apps.brew.formulae", "apps.brew.casks":
		return "brew install"
	case "packages.npm.global":
		return "npm install -g"
	case "packages.pnpm.global":
		return "pnpm add -g"
	case "packages.bun.global":
		return "bun add -g"
	case "packages.pipx":
		return "pipx install"
	case "packages.uv":
		return "uv tool install"
	case "packages.composer":
		return "composer global require"
	case "packages.pub":
		return "dart pub global activate"
	case "packages.dotnet":
		return "dotnet tool install --global"
	case "packages.apt":
		return "sudo apt-get install -y"
	case "packages.dnf":
		return "sudo dnf install -y"
	case "packages.pacman":
		return "sudo pacman -S --needed"
	case "packages.snap":
		return "sudo snap install"
	case "packages.flatpak":
		return "flatpak install -y flathub"
	case "runtimes.rust.crates":
		return "cargo install"
	case "runtimes.rust.toolchains":
		return "rustup toolchain install"
	case "editor.vscode.extensions":
		return "code --install-extension"
	case "editor.cursor.extensions":
		return "cursor --install-extension"
	default:
		return ""
	}
}

// firstToken is the installable name from an item's reported form, so a `fix:`
// command lists installable names rather than a pinned (possibly yanked or
// non-existent) spec. It drops a trailing " version" and an "@version" pin
// (npm/bun/pnpm/pipx/cargo all report name@version), preserving a leading '@'
// for scoped npm packages (@scope/pkg@1.2.3 → @scope/pkg).
func firstToken(s string) string {
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndexByte(s, '@'); i > 0 {
		s = s[:i]
	}
	return s
}

// loadSnapshotArg reads a snapshot file, or the inventory inside a backup
// folder or archive.
func loadSnapshotArg(ctx context.Context, env *sys.OS, path string) (snapshot.Snapshot, error) {
	if strings.HasSuffix(path, ".json") && !isGitHubSpec(path) {
		return parseSnapshotFile(env, path)
	}
	dir, cleanup, err := openBackupOnly(ctx, env, path, "inventory")
	defer cleanup()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(dir, "inventory", "snapshot.json"))
	if err != nil {
		return nil, fmt.Errorf("%s has no inventory (made with --skip inventory, or by an older dothaven)", path)
	}
	return snapshot.Parse(b)
}

func newMissingCmd(env *sys.OS) *cobra.Command {
	return &cobra.Command{
		Use:   "missing [backup-or-snapshot]",
		Short: "What the old machine had installed that this one doesn't",
		Long: "Compares the app & package list inside a backup (or a `collect` snapshot) with\n" +
			"this machine and lists what is missing, with the command that installs each\n" +
			"group. Run it on the new machine after restoring; `dothaven reinstall <backup>`\n" +
			"installs them for you. Exits 1 if anything is missing.",
		Args: cobra.MaximumNArgs(1),
		// A drift result returns a non-zero exit (CI-friendly), which is a normal
		// outcome — not an error to print. The report is already on stdout.
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := ""
			if len(args) > 0 {
				path, _ = absBackupArg(args[0])
			}
			return runMissing(cmd, env, path)
		},
	}
}

// runMissing reports what a backup's (or snapshot's) inventory has that this
// machine lacks. With no path it uses the newest snapshot.
func runMissing(cmd *cobra.Command, env *sys.OS, path string) error {
	if path == "" {
		found := newestSnapshots(env, 1)
		if len(found) == 0 {
			fmt.Println("Which backup? Pass one: dothaven missing <backup>  (or run `dothaven collect` on the old machine).")
			return nil
		}
		path = found[0]
		fmt.Printf("%s\n\n", dim("Using newest snapshot: "+filepath.Base(path)))
	}
	want, err := loadSnapshotArg(cmd.Context(), env, path)
	if err != nil {
		return err
	}
	snap := gatherInventory(cmd.Context(), env)
	if cerr := cmd.Context().Err(); cerr != nil {
		return ExitError{Code: 130} // cancelled mid-collect — a partial snapshot gives a bogus verdict
	}
	missing := findMissing(want, snap)

	ids := make([]string, 0, len(missing))
	for id := range missing {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	if len(ids) == 0 {
		fmt.Println(good("✓ Nothing missing — everything installable in that backup is here."))
		return nil
	}

	fmt.Println(bold("Missing on this machine:"))
	total := 0
	for _, id := range ids {
		items := missing[id]
		total += len(items)
		label := inventoryLabels[id]
		if label == "" {
			label = id
		}
		fmt.Printf("\n  %s %s\n", bold(label), dim(fmt.Sprintf("(%d)", len(items))))
		fmt.Printf("    %s\n", preview(items, 12))
		if rc := remediationCommand(id); rc != "" {
			names := make([]string, len(items))
			for i, it := range items {
				names[i] = firstToken(it)
			}
			fmt.Printf("    %s %s\n", dim("fix:"), kbd(rc+" "+strings.Join(names, " ")))
		}
	}
	fmt.Printf("\n%s missing across %s.\n", plural(total, "item"), plural(len(ids), "group"))
	if !strings.HasSuffix(path, ".json") {
		fmt.Printf("Install them (all, or the ones you pick): %s\n", kbd("dothaven reinstall "+shortHome(env, path)))
	}
	return ExitError{Code: 1}
}
