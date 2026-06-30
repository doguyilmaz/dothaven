package cli

import (
	"fmt"
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

// firstToken is the package name from an item's reported form ("name version" →
// "name"), so a remediation command lists installable names, not version noise.
func firstToken(s string) string {
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i]
	}
	return s
}

func newDoctorCmd(env *sys.OS) *cobra.Command {
	c := &cobra.Command{
		Use:   "doctor <snapshot.json>",
		Short: "Compare a snapshot against this machine; list what's missing",
		Args:  cobra.ExactArgs(1),
		// A drift result returns a non-zero exit (CI-friendly), which is a normal
		// outcome — not an error to print. The report is already on stdout.
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			want, err := parseSnapshotFile(env, args[0])
			if err != nil {
				return err
			}
			snap := gatherSnapshot(cmd.Context(), env, false)
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
				fmt.Println("✅ Parity — everything installable in the snapshot is present on this machine.")
				return nil
			}

			fmt.Println("Missing on this machine (present in the snapshot):")
			fmt.Println()
			total := 0
			for _, id := range ids {
				items := missing[id]
				total += len(items)
				fmt.Printf("  %s (%d)\n", id, len(items))
				for _, it := range items {
					fmt.Printf("    - %s\n", it)
				}
				if cmd := remediationCommand(id); cmd != "" {
					names := make([]string, len(items))
					for i, it := range items {
						names[i] = firstToken(it)
					}
					fmt.Printf("    fix: %s %s\n", cmd, strings.Join(names, " "))
				}
			}
			fmt.Printf("\n%d item(s) missing across %d section(s).\n", total, len(ids))
			return ExitError{Code: 1}
		},
	}
	return c
}
