package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/doguyilmaz/dothaven/internal/backup"
	"github.com/doguyilmaz/dothaven/internal/registry"
	"github.com/doguyilmaz/dothaven/internal/scan"
	"github.com/doguyilmaz/dothaven/internal/sys"
	"github.com/doguyilmaz/dothaven/internal/tui"
	"github.com/spf13/cobra"
)

// backupGroups aggregates backup targets into selectable category groups
// (count per category, and whether any entry in it is high-sensitivity).
func backupGroups(targets []registry.BackupTarget) []tui.Group {
	type agg struct {
		count int
		enc   bool
	}
	m := map[string]*agg{}
	for _, t := range targets {
		a := m[t.Category]
		if a == nil {
			a = &agg{}
			m[t.Category] = a
		}
		a.count++
		if t.Sensitivity == registry.High {
			a.enc = true
		}
	}
	cats := make([]string, 0, len(m))
	for c := range m {
		cats = append(cats, c)
	}
	sort.Strings(cats)
	groups := make([]tui.Group, 0, len(cats))
	for _, c := range cats {
		groups = append(groups, tui.Group{Name: c, Count: m[c].count, Encrypted: m[c].enc})
	}
	return groups
}

// formatCategories renders a per-category count map as "shell (3), git (2)".
func formatCategories(perCat map[string]int) string {
	cats := make([]string, 0, len(perCat))
	for c := range perCat {
		cats = append(cats, c)
	}
	sort.Strings(cats)
	parts := make([]string, len(cats))
	for i, c := range cats {
		parts[i] = fmt.Sprintf("%s (%d)", c, perCat[c])
	}
	return strings.Join(parts, ", ")
}

func newBackupCmd(env *sys.OS) *cobra.Command {
	var noRedact, archive bool
	var output string
	var only, skip []string
	c := &cobra.Command{
		Use:   "backup",
		Short: "Copy tracked config files into a timestamped backup",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			redact := !noRedact

			// A backup is the safety net before wiping a machine — remind the user
			// (on a terminal) that it captures saved config, not unsaved editor work.
			if stdoutIsTTY() {
				fmt.Println("Note: this captures saved config files — not unsaved editor buffers or")
				fmt.Println("in-memory state. Save and quit your editors before relying on it to wipe a machine.")
				fmt.Println()
			}

			// Interactive category picker: only on a terminal, and only when the
			// user hasn't already constrained the set with --only/--skip.
			if len(only) == 0 && len(skip) == 0 && tui.Interactive() {
				chosen, err := tui.SelectCategories("What to back up", backupGroups(registry.BackupTargets(env.Home(), registry.Entries)))
				if err != nil {
					return err
				}
				if len(chosen) == 0 {
					fmt.Println("Nothing selected.")
					return nil
				}
				only = chosen
			}

			dir := env.ResolveOutputDir(output)
			host, _ := os.Hostname()
			if host == "" {
				host = "machine"
			}
			backupDir := filepath.Join(dir, fmt.Sprintf("backup-%s-%s", host, sys.Timestamp(time.Now())))

			targets := registry.BackupTargets(env.Home(), registry.Entries)
			res, err := backup.Run(targets, backupDir, backup.Options{Redact: redact, Only: only, Skip: skip})
			if err != nil {
				return err
			}
			if res.TotalFiles == 0 {
				fmt.Println("No files found to backup.")
				return nil
			}

			// Write a self-describing MANIFEST into the tree (before archiving, so
			// it travels with the backup) — what's inside, what was excluded, and
			// how to restore it.
			manifest := backup.Manifest(backup.ManifestMeta{
				Host:     host,
				OS:       runtime.GOOS,
				Version:  cmd.Root().Version,
				Created:  time.Now().Format(time.RFC3339),
				Redacted: redact,
			}, res)
			if err := sys.WriteFileSecure(filepath.Join(backupDir, "MANIFEST.txt"), manifest); err != nil {
				return err
			}

			summary := formatCategories(res.PerCategory)
			if archive {
				archivePath := backupDir + ".tar.gz"
				tmp := archivePath + ".tmp"
				// runShell surfaces tar's exit code — env.Run tolerates non-zero
				// exit, which would let a partial archive pass and then delete the
				// only good copy. Write to a temp file, rename on success, and only
				// remove the source once a complete archive exists.
				if out, err := runShell(cmd.Context(), "tar", "czf", tmp, "-C", dir, filepath.Base(backupDir)); err != nil {
					_ = os.Remove(tmp)
					return fmt.Errorf("tar failed (backup kept at %s): %v: %s", backupDir, err, out)
				}
				if err := os.Rename(tmp, archivePath); err != nil {
					return err
				}
				_ = os.RemoveAll(backupDir)
				fmt.Printf("Archive saved to: %s\n  %d files across: %s\n", archivePath, res.TotalFiles, summary)
			} else {
				fmt.Printf("Backup saved to: %s\n  %d files across: %s\n", backupDir, res.TotalFiles, summary)
			}

			if len(res.SkippedSensitive) > 0 {
				fmt.Printf("\n⚠ %d high-sensitivity path(s) excluded from this plaintext backup:\n", len(res.SkippedSensitive))
				for _, d := range res.SkippedSensitive {
					fmt.Printf("    %s\n", d)
				}
				fmt.Println("  Carry these with: dothaven chezmoi-export --apply  (age-encrypted)")
			}

			if len(res.ReadErrors) > 0 {
				fmt.Fprintf(os.Stderr, "\n⚠ %d file(s) exist but could not be read — excluded from the backup:\n", len(res.ReadErrors))
				for _, d := range res.ReadErrors {
					fmt.Fprintf(os.Stderr, "    %s\n", d)
				}
			}

			if redact {
				if report := scan.FormatReport(scan.Summarize(res.ScanResults)); strings.TrimSpace(report) != "" {
					fmt.Println(report)
				}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&noRedact, "no-redact", false, "keep raw values (skip secret redaction)")
	c.Flags().BoolVar(&archive, "archive", false, "create a .tar.gz instead of a directory")
	c.Flags().StringVarP(&output, "output", "o", "", "output directory (default: ./reports in a repo, else ~/Downloads)")
	c.Flags().StringSliceVar(&only, "only", nil, "only these categories (comma-separated)")
	c.Flags().StringSliceVar(&skip, "skip", nil, "skip these categories (comma-separated)")
	return c
}
