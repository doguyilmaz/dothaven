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
			missing := findMissing(want, gatherSnapshot(cmd.Context(), env, false))

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
			}
			fmt.Printf("\n%d item(s) missing across %d section(s).\n", total, len(ids))
			return ExitError{Code: 1}
		},
	}
	return c
}
