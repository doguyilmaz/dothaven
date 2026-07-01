package cli

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/doguyilmaz/dothaven/internal/scan"
	"github.com/doguyilmaz/dothaven/internal/sys"
	"github.com/spf13/cobra"
)

// curatedServiceConfigs are Homebrew-managed local service config files, relative
// to $(brew --prefix)/etc. These are user-editable dev config (not data, not
// system /etc), so they're in scope; the service binary itself comes back via
// the Brewfile.
func curatedServiceConfigs() []string {
	return []string{
		"nginx/nginx.conf",
		"httpd/httpd.conf",
		"my.cnf",
		"redis.conf",
		"dnsmasq.conf",
	}
}

const (
	servicesSubdir     = "services"
	servicesPrefixFile = ".brew-prefix"
)

func brewPrefix(ctx context.Context) string {
	out, err := runShell(ctx, "brew", "--prefix")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// rewriteServicePrefix re-points an exported config at the importing machine's
// Homebrew prefix (Intel /usr/local vs ARM /opt/homebrew vs Linuxbrew), so paths
// baked into the config resolve on the new host. A no-op when the prefix matches.
func rewriteServicePrefix(content, oldPrefix, newPrefix string) string {
	if oldPrefix == "" || oldPrefix == newPrefix {
		return content
	}
	return strings.ReplaceAll(content, oldPrefix, newPrefix)
}

func newServicesCmd(env *sys.OS) *cobra.Command {
	c := &cobra.Command{
		Use:   "services",
		Short: "Capture and restore Homebrew-managed local service config",
		Long:  "Exports user-editable service config under $(brew --prefix)/etc (nginx, httpd,\nmysql, redis, dnsmasq) and re-imports it on a new machine, re-pointing the\nHomebrew prefix so Intel/ARM/Linuxbrew paths resolve. The service binaries\nthemselves come back via the Brewfile; databases and other data are out of scope.",
		Args:  cobra.NoArgs,
	}
	c.AddCommand(newServicesExportCmd(env), newServicesImportCmd(env))
	return c
}

func newServicesExportCmd(env *sys.OS) *cobra.Command {
	var output string
	c := &cobra.Command{
		Use:   "export",
		Short: "Export Homebrew service config (nginx, mysql, …)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			prefix := brewPrefix(ctx)
			if prefix == "" {
				fmt.Println("Homebrew not found — nothing to export (this captures $(brew --prefix)/etc service config).")
				return nil
			}
			dir := filepath.Join(env.ResolveOutputDir(output), servicesSubdir)
			n := 0
			for _, rel := range curatedServiceConfigs() {
				raw, err := env.ReadFile(filepath.Join(prefix, "etc", rel))
				if err != nil {
					continue
				}
				content := string(raw)
				// Captured verbatim so it round-trips; warn (don't redact) if it
				// looks secret-bearing, e.g. a password in my.cnf.
				if scan.ScanContent(rel, content).Action != scan.Include {
					fmt.Printf("  ⚠ %s may contain secrets — captured verbatim; keep this export safe.\n", rel)
				}
				if err := sys.WriteFileSecure(filepath.Join(dir, rel), content); err != nil {
					return err
				}
				fmt.Printf("  ✔ %s\n", rel)
				n++
			}
			if n == 0 {
				fmt.Printf("No service config found under %s.\n", filepath.Join(prefix, "etc"))
				return nil
			}
			if err := sys.WriteFileSecure(filepath.Join(dir, servicesPrefixFile), prefix); err != nil {
				return err
			}
			fmt.Printf("Exported %d service config(s) to %s\n", n, dir)
			fmt.Println("Replay on a new machine with: dothaven services import <dir>")
			return nil
		},
	}
	c.Flags().StringVarP(&output, "output", "o", "", "output directory (default: ./reports in a repo, else ~/.local/share/dothaven)")
	return c
}

func newServicesImportCmd(env *sys.OS) *cobra.Command {
	return &cobra.Command{
		Use:   "import <dir>",
		Short: "Import Homebrew service config into this machine's $(brew --prefix)/etc",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			newPrefix := brewPrefix(ctx)
			if newPrefix == "" {
				return fmt.Errorf("Homebrew not found — can't resolve $(brew --prefix) to import into")
			}
			dir := args[0]
			if _, err := env.ListDir(filepath.Join(dir, servicesSubdir)); err == nil {
				dir = filepath.Join(dir, servicesSubdir)
			}
			oldPrefix := newPrefix
			if b, err := env.ReadFile(filepath.Join(dir, servicesPrefixFile)); err == nil {
				oldPrefix = strings.TrimSpace(string(b))
			}
			n := 0
			walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				rel, _ := filepath.Rel(dir, path)
				if rel == servicesPrefixFile {
					return nil
				}
				raw, rerr := os.ReadFile(path)
				if rerr != nil {
					return nil
				}
				content := rewriteServicePrefix(string(raw), oldPrefix, newPrefix)
				if werr := sys.WriteFile(filepath.Join(newPrefix, "etc", rel), content); werr != nil {
					return werr
				}
				fmt.Printf("  ✔ %s\n", rel)
				n++
				return nil
			})
			if walkErr != nil {
				return walkErr
			}
			fmt.Printf("Imported %d service config(s) into %s. Restart the affected services.\n", n, filepath.Join(newPrefix, "etc"))
			return nil
		},
	}
}
