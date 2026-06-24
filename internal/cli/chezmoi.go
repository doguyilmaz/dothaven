package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/doguyilmaz/dothaven/internal/chezmoi"
	"github.com/doguyilmaz/dothaven/internal/collect"
	"github.com/doguyilmaz/dothaven/internal/registry"
	"github.com/doguyilmaz/dothaven/internal/scan"
	"github.com/doguyilmaz/dothaven/internal/snapshot"
	"github.com/doguyilmaz/dothaven/internal/sys"
	"github.com/doguyilmaz/dothaven/internal/tui"
	"github.com/spf13/cobra"
)

// runShell executes a command surfacing its exit status (unlike sys.Env.Run,
// which tolerates non-zero exit for collectors). Used only on the --apply path.
// A per-command timeout keeps a hung/prompting chezmoi from blocking forever.
func runShell(ctx context.Context, name string, args ...string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 2*sys.CommandTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// templatizeSource rewrites the just-added chezmoi source template for src,
// replacing absolute home paths with chezmoi's homeDir variable so the config
// ports to a new machine. Best-effort: any lookup/read/write failure leaves the
// template as a verbatim copy (still valid), so it never fails the export.
func templatizeSource(ctx context.Context, src, home string) {
	sp, err := runShell(ctx, "chezmoi", "source-path", src)
	if err != nil || sp == "" {
		return
	}
	raw, err := os.ReadFile(sp)
	if err != nil {
		return
	}
	if out, changed := chezmoi.Templatize(string(raw), home); changed {
		_ = os.WriteFile(sp, []byte(out), 0o644)
	}
}

func planHasSrc(plan []chezmoi.PlanItem, src string) bool {
	for _, p := range plan {
		if p.Src == src {
			return true
		}
	}
	return false
}

func planHasID(plan []chezmoi.PlanItem, id string) bool {
	for _, p := range plan {
		if p.ID == id {
			return true
		}
	}
	return false
}

func removeByID(plan []chezmoi.PlanItem, id string) []chezmoi.PlanItem {
	out := plan[:0]
	for _, p := range plan {
		if p.ID != id {
			out = append(out, p)
		}
	}
	return out
}

func gatherInstallManifest(ctx context.Context, env *sys.OS, pin bool) chezmoi.Manifest {
	cctx := collect.Ctx{Context: ctx, Env: env, Home: env.Home(), Redact: false}
	brew := collect.HomebrewCollector(cctx)
	pkgs := collect.PackagesCollector(cctx)
	runtimes := collect.RuntimesCollector(cctx)
	exts := collect.EditorsExtCollector(cctx)

	specs := func(snap snapshot.Snapshot, id string) []string {
		var out []string
		for _, it := range snap[id].Items {
			if s := chezmoi.PickInstallSpec(it, pin); s != "" {
				out = append(out, s)
			}
		}
		return out
	}

	// The Brewfile is embedded verbatim into an UNENCRYPTED script — redact any
	// inline credentials (e.g. a private tap's https://user:pass@host) first.
	var brewfile string
	if c := brew["apps.brew.bundle"].Content; c != nil {
		brewfile = scan.ApplyRedactions(*c, scan.ScanContent("Brewfile", *c))
	}

	var nodeVersions []string // node always keeps its exact version
	for _, it := range pkgs["packages.node.fnm"].Items {
		if len(it.Columns) > 0 {
			nodeVersions = append(nodeVersions, it.Columns[0])
		}
	}

	return chezmoi.Manifest{
		Brewfile:         brewfile,
		NodeVersions:     nodeVersions,
		BunGlobals:       specs(pkgs, "packages.bun.global"),
		NpmGlobals:       specs(pkgs, "packages.npm.global"),
		PnpmGlobals:      specs(pkgs, "packages.pnpm.global"),
		CargoCrates:      specs(runtimes, "runtimes.rust.crates"),
		DenoBins:         specs(pkgs, "packages.deno.bin"),
		PipxPackages:     specs(pkgs, "packages.pipx"),
		CursorExtensions: specs(exts, "editor.cursor.extensions"),
		RustToolchains:   specs(runtimes, "runtimes.rust.toolchains"),
		UvTools:          specs(pkgs, "packages.uv"),
		ComposerGlobals:  specs(pkgs, "packages.composer"),
		PubGlobals:       specs(pkgs, "packages.pub"),
		DotnetTools:      specs(pkgs, "packages.dotnet"),
	}
}

func newChezmoiExportCmd(env *sys.OS) *cobra.Command {
	var apply, pin bool
	var only, skip []string
	c := &cobra.Command{
		Use:   "chezmoi-export",
		Short: "Plan (or apply) adding configs to chezmoi, encrypting secrets",
		Long:  "Builds a chezmoi-add plan — plain for configs, --encrypt for secrets — plus a\nrun_onchange install script. Dry-run by default; --apply executes (needs chezmoi + age).",
		Args:  cobra.NoArgs,
		// A partial-apply failure returns a message-less ExitError after printing
		// its own diagnostics; don't let cobra also print an "Error:" line.
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			home := env.Home()
			ctx := cmd.Context()

			// Interactive picker on a terminal with no explicit filter. The
			// install groups (brew, packages) sit alongside config categories.
			if len(only) == 0 && len(skip) == 0 && tui.Interactive() {
				groups := backupGroups(registry.BackupTargets(home, registry.Entries))
				groups = append(groups, tui.Group{Name: "brew"}, tui.Group{Name: "packages"})
				chosen, err := tui.SelectCategories("What to export to chezmoi", groups)
				if err != nil {
					return err
				}
				if len(chosen) == 0 {
					fmt.Println("Nothing selected.")
					return nil
				}
				only = chosen
			}

			wantBrew := chezmoi.IsSelected("brew", only, skip)
			wantPackages := chezmoi.IsSelected("packages", only, skip)
			wantInstallScript := wantBrew || wantPackages

			var entries []registry.Entry
			for _, e := range registry.Entries {
				if chezmoi.IsSelected(e.Category, only, skip) {
					entries = append(entries, e)
				}
			}
			plan := chezmoi.PlanExport(entries, home, env.Exists, chezmoi.ContainsHighSecret)

			if chezmoi.IsSelected("ssh", only, skip) {
				for _, key := range chezmoi.FindSshPrivateKeys(home, env.ListDir, chezmoi.IsSSHPrivateKey) {
					if !planHasSrc(plan, key) {
						plan = append(plan, chezmoi.PlanItem{ID: "ssh.key", Src: key, Kind: "file", Encrypt: true, Reason: "ssh private key"})
					}
				}
			}

			if !chezmoi.GnupgHasSecretKeys(home, env.ListDir) {
				plan = removeByID(plan, "secrets.gnupg")
			}

			if len(plan) == 0 && !wantInstallScript {
				fmt.Println("Nothing to export — no managed configs found on this machine.")
				return nil
			}

			hasEncrypt := false
			for _, p := range plan {
				if p.Encrypt {
					hasEncrypt = true
					break
				}
			}

			if len(plan) > 0 {
				encrypted := 0
				for _, p := range plan {
					if p.Encrypt {
						encrypted++
					}
				}
				fmt.Printf("chezmoi-export plan — %d path(s), %d encrypted:\n\n", len(plan), encrypted)
				for _, p := range plan {
					verb := "   add          "
					switch {
					case p.Encrypt:
						verb = "🔒 add --encrypt"
					case p.Template:
						verb = "📝 add --template"
					}
					fmt.Printf("  %s  %s  (%s)\n", verb, p.Src, p.Reason)
				}
			}
			if wantInstallScript {
				var groups []string
				if wantBrew {
					groups = append(groups, "brew")
				}
				if wantPackages {
					groups = append(groups, "packages")
				}
				fmt.Printf("  + run_onchange install script (%s)\n", strings.Join(groups, ", "))
			}

			if conflicts := chezmoi.SettingsSyncConflicts(plan, env.Exists); len(conflicts) > 0 {
				fmt.Println("\n⚠ Editor Settings Sync looks active — chezmoi and the editor's cloud sync will")
				fmt.Println("  both rewrite these, causing drift. Disable one (chezmoi-managed or Settings Sync):")
				for _, c := range conflicts {
					fmt.Printf("    %s\n", c)
				}
			}

			if hasEncrypt {
				fmt.Printf("\n🔒 Encrypted paths are recoverable only with your age key (%s/.config/chezmoi/key.txt).\n   Back it up offline before you rely on this — a lost key means those files are gone for good.\n", home)
			}

			if !apply {
				fmt.Println("\nDry-run. Re-run with --apply to execute (requires chezmoi + a configured age key).")
				return nil
			}

			if v, err := runShell(ctx, "chezmoi", "--version"); err != nil || v == "" {
				fmt.Fprintln(os.Stderr, "\nchezmoi not found. Install it (brew install chezmoi) and configure age encryption first.")
				return ExitError{Code: 1}
			}
			sourcePath, _ := runShell(ctx, "chezmoi", "source-path")

			// Preflight: if the plan encrypts anything, age must be configured —
			// otherwise the very first `add --encrypt` fails. Abort early with a
			// clear message instead of a confusing per-file error.
			if hasEncrypt {
				configured := false
				if b, err := os.ReadFile(home + "/.config/chezmoi/chezmoi.toml"); err == nil {
					configured = ageEncryptionRe.Match(b)
				}
				if !configured {
					fmt.Fprintln(os.Stderr, "\n✗ This plan encrypts secrets, but age encryption is not configured in chezmoi.toml.")
					fmt.Fprintln(os.Stderr, "  Run `dothaven init`, configure your age key, then re-run with --apply.")
					return ExitError{Code: 1}
				}

				// Age-key safety rail: encrypted secrets are recoverable only with
				// the age key, so a human must acknowledge it's backed up before we
				// write ciphertext they could otherwise lose forever. CI/non-TTY
				// can't be prompted — the warning above still printed for them.
				if tui.Interactive() {
					ok, err := tui.Confirm("Have you backed up your age key offline?")
					if err != nil {
						return err
					}
					if !ok {
						fmt.Fprintln(os.Stderr, "\nAborted. Back up your age key first, then re-run with --apply.")
						return ExitError{Code: 1}
					}
				}
			}

			if sourcePath != "" && planHasID(plan, "secrets.gnupg") {
				ignorePath := sourcePath + "/.chezmoiignore"
				existing := ""
				if b, err := os.ReadFile(ignorePath); err == nil {
					existing = string(b)
				}
				if err := os.WriteFile(ignorePath, []byte(chezmoi.MergeChezmoiignore(existing, chezmoi.GnupgIgnorePatterns())), 0o644); err != nil {
					return err
				}
				fmt.Println("  ✔ .chezmoiignore (gnupg runtime cruft)")
			}

			fmt.Println("")
			var failed, failedEncrypted int
			for _, p := range plan {
				addArgs := []string{"add", p.Src}
				switch {
				case p.Encrypt:
					addArgs = []string{"add", "--encrypt", p.Src}
				case p.Template:
					addArgs = []string{"add", "--template", p.Src}
				}
				if out, err := runShell(ctx, "chezmoi", addArgs...); err != nil {
					fmt.Fprintf(os.Stderr, "  ✗ %s: %v %s\n", p.Src, err, out)
					failed++
					if p.Encrypt {
						failedEncrypted++
					}
					continue
				}
				if p.Template {
					templatizeSource(ctx, p.Src, home)
				}
				prefix := ""
				switch {
				case p.Encrypt:
					prefix = "encrypted "
				case p.Template:
					prefix = "templated "
				}
				fmt.Printf("  ✔ %s%s\n", prefix, p.Src)
			}

			if wantInstallScript {
				manifest := gatherInstallManifest(ctx, env, pin)
				if dupes := chezmoi.CrossManagerDuplicates(manifest); len(dupes) > 0 {
					fmt.Fprintf(os.Stderr, "  ⚠ installed by multiple managers (review for PATH shadowing): %s\n", strings.Join(dupes, ", "))
				}
				if !wantBrew {
					manifest.Brewfile = ""
				} else if manifest.Brewfile != "" {
					manifest.Brewfile = chezmoi.FilterBrewfile(manifest.Brewfile, skip)
				}
				if !wantPackages {
					manifest.NodeVersions, manifest.BunGlobals, manifest.NpmGlobals = nil, nil, nil
					manifest.PnpmGlobals, manifest.CargoCrates, manifest.DenoBins = nil, nil, nil
					manifest.PipxPackages, manifest.CursorExtensions, manifest.RustToolchains = nil, nil, nil
					manifest.UvTools, manifest.ComposerGlobals, manifest.PubGlobals, manifest.DotnetTools = nil, nil, nil, nil
				}
				if script, ok := chezmoi.BuildPackageInstallScript(manifest); ok && sourcePath != "" {
					if err := os.WriteFile(sourcePath+"/run_onchange_install-packages.sh", []byte(script), 0o755); err != nil {
						fmt.Fprintf(os.Stderr, "  ✗ install script: %v\n", err)
						failed++
					} else {
						fmt.Println("  ✔ run_onchange_install-packages.sh")
					}
				}
			}

			if failed > 0 {
				if failedEncrypted > 0 {
					fmt.Fprintf(os.Stderr, "\n✗ %d operation(s) failed — %d were encrypted secrets that were NOT carried.\n", failed, failedEncrypted)
				} else {
					fmt.Fprintf(os.Stderr, "\n✗ %d operation(s) failed.\n", failed)
				}
				fmt.Fprintln(os.Stderr, "Fix the errors above, then re-run `dothaven chezmoi-export --apply`.")
				return ExitError{Code: 1}
			}

			fmt.Println("\nDone. Review with `chezmoi diff`, then commit your private chezmoi source repo.")
			return nil
		},
	}
	c.Flags().BoolVar(&apply, "apply", false, "execute the plan (default: dry-run)")
	c.Flags().BoolVar(&pin, "pin", false, "pin global packages to their captured version")
	c.Flags().StringSliceVar(&only, "only", nil, "only these categories/groups (comma-separated)")
	c.Flags().StringSliceVar(&skip, "skip", nil, "skip these categories/groups (comma-separated)")
	return c
}
