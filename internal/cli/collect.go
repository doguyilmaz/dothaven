package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/doguyilmaz/dothaven/internal/collect"
	"github.com/doguyilmaz/dothaven/internal/registry"
	"github.com/doguyilmaz/dothaven/internal/scan"
	"github.com/doguyilmaz/dothaven/internal/snapshot"
	"github.com/doguyilmaz/dothaven/internal/sys"
	"github.com/spf13/cobra"
)

// defaultCollectors is the canonical inventory pipeline: meta first (it labels
// the snapshot with host/OS), then the declarative registry, then every
// command-backed collector. The registry adapter reads Redact from the Ctx so
// the same list serves `collect` (redacting) and `doctor` (raw).
func defaultCollectors() []collect.Collector {
	return []collect.Collector{
		collect.MetaCollector,
		func(c collect.Ctx) snapshot.Snapshot {
			return registry.Collect(c.Context, c.Env, c.Home, c.Redact, registry.Entries)
		},
		collect.SSHCollector,
		collect.OllamaCollector,
		collect.AppsCollector,
		collect.HomebrewCollector,
		collect.PackagesCollector,
		collect.LinuxPackagesCollector,
		collect.VersionManagersCollector,
		collect.RuntimesCollector,
		collect.EditorsExtCollector,
		collect.FontsCollector,
		collect.MobileCollector,
		collect.ScheduledCollector,
		collect.DotfilesSweepCollector,
	}
}

// gatherInventory is the collector pipeline minus the registry file reads:
// what is installed, not what the config files say. A backup already carries
// those files, and copying their contents a second time into the inventory
// would double the size and the places a secret can sit.
func gatherInventory(ctx context.Context, env *sys.OS) snapshot.Snapshot {
	var cols []collect.Collector
	for i, c := range defaultCollectors() {
		if i != 1 { // defaultCollectors()[1] is the registry adapter
			cols = append(cols, c)
		}
	}
	var done int64
	stop := startProgress("listing installed apps & packages", &done, len(cols))
	defer stop()
	return collect.RunCollectors(collect.Ctx{Context: ctx, Env: env, Home: env.Home(), Done: &done}, cols)
}

// gatherSnapshot runs the full collector pipeline against the live machine. The
// context (from the command, signal-aware) bounds and cancels the run.
func gatherSnapshot(ctx context.Context, env *sys.OS, redact bool) snapshot.Snapshot {
	collectors := defaultCollectors()
	var done int64
	stop := startProgress("inventorying", &done, len(collectors))
	defer stop()
	return collect.RunCollectors(collect.Ctx{
		Context: ctx,
		Env:     env,
		Home:    env.Home(),
		Redact:  redact,
		Done:    &done,
	}, collectors)
}

const slimMaxLines = 10

// slimSections truncates long file contents in place, keeping snapshots readable
// when the full body isn't needed.
func slimSections(s snapshot.Snapshot) {
	for name, sec := range s {
		if sec.Content == nil {
			continue
		}
		lines := strings.Split(*sec.Content, "\n")
		if len(lines) <= slimMaxLines {
			continue
		}
		trimmed := strings.Join(lines[:slimMaxLines], "\n") +
			fmt.Sprintf("\n... (%d more lines)", len(lines)-slimMaxLines)
		sec.Content = &trimmed
		s[name] = sec
	}
}

func newCollectCmd(env *sys.OS) *cobra.Command {
	var noRedact, slim bool
	var output string
	c := &cobra.Command{
		Use:   "collect",
		Short: "Inventory this machine into a timestamped JSON snapshot",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			redact := !noRedact
			snap := gatherSnapshot(cmd.Context(), env, redact)
			if cerr := cmd.Context().Err(); cerr != nil {
				fmt.Fprintln(os.Stderr, "collect cancelled.")
				return ExitError{Code: 130} // cancelled mid-collect: don't write a truncated snapshot as success
			}

			var scanResults []scan.Result
			if redact {
				for name, sec := range snap {
					kept, results := scan.RedactSection(name, &sec)
					scanResults = append(scanResults, results...)
					if kept {
						snap[name] = sec
					} else {
						delete(snap, name)
					}
				}
			}

			if slim {
				slimSections(snap)
			}

			data, err := snap.Serialize()
			if err != nil {
				return err
			}

			dir := output
			if dir == "" {
				dir = snapshotDir(env)
			}
			path := filepath.Join(dir, fmt.Sprintf("%s-%s.json", hostname(), sys.Timestamp(time.Now())))
			if err := sys.WriteFileSecure(path, string(data)); err != nil {
				return err
			}
			fmt.Printf("%s %s\n", bold("Snapshot saved to:"), shortHome(env, path))
			fmt.Printf("  %s\n", dim(fmt.Sprintf("%d sections. Browse with `dothaven list <section>`, e.g. `dothaven list brew`.", len(snap))))

			if redact {
				if report := scan.FormatReport(scan.Summarize(scanResults), scan.ReportOptions{Color: colorOn()}); strings.TrimSpace(report) != "" {
					fmt.Println(report)
				}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&noRedact, "no-redact", false, "keep raw values (skip secret redaction)")
	c.Flags().BoolVar(&slim, "slim", false, "truncate long file contents to 10 lines")
	c.Flags().StringVarP(&output, "output", "o", "", "output directory (default: ~/.local/share/dothaven/snapshots)")
	return c
}
