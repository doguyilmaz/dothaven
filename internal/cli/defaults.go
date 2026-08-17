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
			// The wide pass: every domain on the machine, key by key, so the
			// settings somebody actually changed — natural scroll, hot
			// corners, Finder options — come along too. Whole-domain import
			// cannot do this safely, which is why it is a separate mechanism.
			domains := listPrefDomains(ctx)
			entries, counts := capturePrefs(ctx, domains)
			if len(entries) > 0 {
				if err := writePrefs(filepath.Join(dir, prefsFileName), entries, counts); err != nil {
					return err
				}
			}

			if n == 0 && len(entries) == 0 {
				fmt.Println("No macOS preferences captured (macOS-only; needs the `defaults` tool).")
				return nil
			}
			fmt.Printf("\n%s as plists, %s from %s.\n",
				plural(n, "app domain"), plural(counts.Apply, "portable setting"), plural(len(domains), "domain"))
			if counts.Review > 0 {
				fmt.Printf("  %s %s captured for review (paths and identifiers from this Mac).\n",
					warn("⚠"), plural(counts.Review, "setting"))
			}
			if counts.Secret > 0 {
				fmt.Printf("  %s %s looked secret-shaped: redacted or dropped.\n",
					warn("⚠"), plural(counts.Secret, "value"))
			}
			fmt.Printf("  %s app state and bookkeeping ignored.\n", dim(plural(counts.Skipped, "key")))
			fmt.Printf("\nWritten to %s\n", dir)
			fmt.Println("Replay on a new machine with: dothaven defaults import <dir>")
			return nil
		},
	}
	c.Flags().StringVarP(&output, "output", "o", "", "output directory (default: ./reports in a repo, else ~/.local/share/dothaven)")
	return c
}

func newDefaultsImportCmd(env *sys.OS) *cobra.Command {
	var dryRun, assumeYes, allDomains bool
	c := &cobra.Command{
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

			// Planned first so --dry-run reports exactly what the write pass
			// would do, rather than a second guess at it.
			type item struct{ domain, path string }
			var plan []item
			for _, name := range names {
				if strings.HasSuffix(name, ".plist") {
					plan = append(plan, item{defaultsDomainFromFile(name), filepath.Join(dir, name)})
				}
			}
			// The per-key half, if the export wrote one. Done first: these are
			// the system settings somebody notices missing, and they must not
			// be skipped just because no app plists came along.
			pf, prefsErr := readPrefs(filepath.Join(dir, prefsFileName))
			if prefsErr == nil && len(pf.Entries) > 0 {
				printHeader("System preferences")
				if err := applyPrefs(ctx, pf.Entries, dryRun, assumeYes, allDomains); err != nil {
					return err
				}
			}

			if len(plan) == 0 {
				if prefsErr == nil {
					return nil
				}
				fmt.Printf("No .plist files in %s — nothing to import.\n", dir)
				return nil
			}

			if prefsErr == nil {
				printHeader("App preference domains")
			}
			fmt.Printf("Will replace preferences for %d domain(s):\n", len(plan))
			for _, it := range plan {
				fmt.Printf("  %s\n", it.domain)
			}
			if dryRun {
				fmt.Println("\nNothing written (--dry-run). Re-run without it to import.")
				return nil
			}
			// `defaults import` replaces a domain wholesale, so this overwrites
			// whatever those apps currently have.
			if err := confirmWrite(os.Stderr, "Replace these preference domains on this machine?", assumeYes); err != nil {
				return err
			}

			n := 0
			for _, it := range plan {
				if out, err := runShell(ctx, "defaults", "import", it.domain, it.path); err != nil {
					fmt.Fprintf(os.Stderr, "  %s %s: %v %s\n", danger("✗"), it.domain, err, out)
					continue
				}
				fmt.Printf("  %s %s\n", good("✔"), it.domain)
				n++
			}
			fmt.Printf("Imported %d domain(s). Restart the affected apps to pick up the new prefs.\n", n)
			return nil
		},
	}
	c.Flags().BoolVar(&dryRun, "dry-run", false, "list the domains that would be replaced, write nothing")
	c.Flags().BoolVar(&allDomains, "all", false,
		"also write settings outside the core system domains (mostly application state)")
	c.Flags().BoolVar(&assumeYes, "yes", false, "skip the confirmation (required off a terminal)")
	return c
}
