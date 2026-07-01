package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/doguyilmaz/dothaven/internal/sys"
	"github.com/spf13/cobra"
)

// macDefaultsDomain is one macOS preference domain captured wholesale via
// `defaults export` and replayed with `defaults import`.
type macDefaultsDomain struct {
	ID   string
	Name string
}

// macDefaultsDomains is the curated allowlist. Deliberately app-level prefs that
// are portable as a whole — iTerm2/Terminal profiles, window managers, hotkey
// tools. System domains (com.apple.dock, com.apple.finder, NSGlobalDomain) are
// intentionally excluded: they carry host-specific keys (display/Spaces UUIDs,
// absolute screenshot paths) that corrupt a new machine if imported verbatim,
// and need per-key curation — a separate step.
func macDefaultsDomains() []macDefaultsDomain {
	return []macDefaultsDomain{
		{"com.googlecode.iterm2", "iTerm2"},
		{"com.apple.Terminal", "Terminal.app"},
		{"com.knollsoft.Rectangle", "Rectangle"},
		{"com.knollsoft.Hookshot", "Rectangle Pro"},
		{"org.hammerspoon.Hammerspoon", "Hammerspoon"},
		{"com.lwouis.alt-tab-macos", "AltTab"},
	}
}

// defaultsFileName / defaultsDomainFromFile map a domain to its capture filename
// and back, so export and import agree.
func defaultsFileName(domain string) string { return domain + ".plist" }
func defaultsDomainFromFile(name string) string {
	return strings.TrimSuffix(name, ".plist")
}

// defaultsHasKeys reports whether an exported plist actually holds preferences.
// `defaults export` of an absent domain returns an empty <dict/>, which we skip.
func defaultsHasKeys(plist string) bool { return strings.Contains(plist, "<key>") }

func newDefaultsCmd(env *sys.OS) *cobra.Command {
	c := &cobra.Command{
		Use:   "defaults",
		Short: "Capture and restore curated macOS app preferences",
		Long:  "Exports a curated set of macOS app preference domains (iTerm2, Terminal,\nwindow managers, …) to plist files, and re-imports them on a new machine via\n`defaults import` — the safe round-trip for cfprefsd-managed prefs. System\ndomains (Dock/Finder/keyboard) are out of scope for now (host-specific keys).",
		Args:  cobra.NoArgs,
	}
	c.AddCommand(newDefaultsExportCmd(env), newDefaultsImportCmd(env))
	return c
}

func newDefaultsExportCmd(env *sys.OS) *cobra.Command {
	var output string
	c := &cobra.Command{
		Use:   "export",
		Short: "Export curated macOS defaults to plist files",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			dir := filepath.Join(env.ResolveOutputDir(output), "macos-defaults")
			n := 0
			for _, d := range macDefaultsDomains() {
				out, err := runShell(ctx, "defaults", "export", d.ID, "-")
				if err != nil || !defaultsHasKeys(out) {
					continue
				}
				if err := sys.WriteFileSecure(filepath.Join(dir, defaultsFileName(d.ID)), out); err != nil {
					return err
				}
				fmt.Printf("  ✔ %s (%s)\n", d.Name, d.ID)
				n++
			}
			if n == 0 {
				fmt.Println("No macOS preferences captured (macOS-only; needs the `defaults` tool).")
				return nil
			}
			fmt.Printf("Exported %d domain(s) to %s\n", n, dir)
			fmt.Println("Replay on a new machine with: dothaven defaults import <dir>")
			return nil
		},
	}
	c.Flags().StringVarP(&output, "output", "o", "", "output directory (default: ./reports in a repo, else ~/.local/share/dothaven)")
	return c
}

func newDefaultsImportCmd(env *sys.OS) *cobra.Command {
	return &cobra.Command{
		Use:   "import <dir>",
		Short: "Import previously exported macOS defaults",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			dir := args[0]
			// Accept either the macos-defaults dir or the parent that contains it.
			if names, _ := env.ListDir(filepath.Join(dir, "macos-defaults")); len(names) > 0 {
				dir = filepath.Join(dir, "macos-defaults")
			}
			names, err := env.ListDir(dir)
			if err != nil {
				return fmt.Errorf("no defaults to import in %s", dir)
			}
			n := 0
			for _, name := range names {
				if !strings.HasSuffix(name, ".plist") {
					continue
				}
				domain := defaultsDomainFromFile(name)
				if out, err := runShell(ctx, "defaults", "import", domain, filepath.Join(dir, name)); err != nil {
					fmt.Fprintf(os.Stderr, "  ✗ %s: %v %s\n", domain, err, out)
					continue
				}
				fmt.Printf("  ✔ %s\n", domain)
				n++
			}
			fmt.Printf("Imported %d domain(s). Restart the affected apps to pick up the new prefs.\n", n)
			return nil
		},
	}
}
