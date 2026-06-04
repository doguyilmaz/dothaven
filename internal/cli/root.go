// Package cli wires the dothaven subcommands onto a Cobra root.
package cli

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/doguyilmaz/dothaven/internal/snapshot"
	"github.com/doguyilmaz/dothaven/internal/sys"
	"github.com/spf13/cobra"
)

// NewRoot builds the root command with every subcommand wired in.
func NewRoot(env *sys.OS, version string) *cobra.Command {
	root := &cobra.Command{
		Use:     "dothaven",
		Short:   "Discover, back up, and migrate your machine's dev config",
		Long:    "dothaven inventories your machine's dev configuration, scans for secrets, and feeds\nchezmoi (age-encrypted) for migration across machines.",
		Version: version,
		// Subcommand errors are returned via RunE; don't dump usage on them.
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	root.AddCommand(
		newScanCmd(env),
		newSecurityCmd(env),
		newCompareCmd(env),
		newListCmd(env),
	)
	return root
}

// --- shared helpers ---

func cwd() string {
	d, _ := os.Getwd()
	return d
}

// stdoutIsTTY reports whether stdout is a terminal (for color), with no deps.
func stdoutIsTTY() bool {
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// newestJSON returns up to n .json files in dir, newest (by mtime) first.
func newestJSON(dir string, n int) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	type fe struct {
		path string
		mod  time.Time
	}
	var files []fe
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fe{filepath.Join(dir, e.Name()), info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })
	var out []string
	for i := 0; i < len(files) && i < n; i++ {
		out = append(out, files[i].path)
	}
	return out
}

func parseSnapshotFile(env *sys.OS, path string) (snapshot.Snapshot, error) {
	b, err := env.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return snapshot.Parse(b)
}

// label is a snapshot file's basename without the .json extension.
func label(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ".json")
}

func sortedStringKeys(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func sortStrings(s []string) { sort.Strings(s) }
