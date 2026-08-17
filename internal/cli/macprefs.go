package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/doguyilmaz/dothaven/internal/macprefs"
	"github.com/doguyilmaz/dothaven/internal/sys"
)

// prefsFileName is the wide per-key capture, written beside the whole-domain
// plists. Two mechanisms, because they are good at different things: a plist
// round-trip preserves an app's nested profiles, and per-key writes are the
// only safe way to move system settings, whose domains carry display and Spaces
// identifiers that must not follow you to a new Mac.
const prefsFileName = "prefs.json"

// prefsFile is what gets written to disk.
type prefsFile struct {
	Counts  macprefs.Counts  `json:"counts"`
	Entries []macprefs.Entry `json:"entries"`
}

// prefsWorkers bounds the fan-out. Each domain costs one `defaults export`, and
// there are several hundred of them; serial takes the better part of a minute.
const prefsWorkers = 8

// listPrefDomains returns every preference domain on this machine.
func listPrefDomains(ctx context.Context) []string {
	out, err := runShell(ctx, "defaults", "domains")
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var domains []string
	// `defaults domains` prints one comma-separated line.
	for _, d := range strings.Split(out, ",") {
		if d = strings.TrimSpace(d); d != "" && !seen[d] {
			seen[d] = true
			domains = append(domains, d)
		}
	}
	// The global domain holds keyboard, scrolling and appearance settings and
	// is not in the list, because it has no application of its own.
	if !seen["NSGlobalDomain"] {
		domains = append(domains, "NSGlobalDomain")
	}
	sort.Strings(domains)
	return domains
}

// capturePrefs reads and classifies every domain.
func capturePrefs(ctx context.Context, domains []string) ([]macprefs.Entry, macprefs.Counts) {
	var (
		mu     sync.Mutex
		all    []macprefs.Entry
		counts macprefs.Counts
		done   int64
		workCh = make(chan string)
		wg     sync.WaitGroup
	)

	stop := startProgress("reading preference domains", &done, len(domains))
	for i := 0; i < prefsWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for d := range workCh {
				// A domain that cannot be read is a domain we do not have, not
				// a reason to abandon the other several hundred.
				if out, err := runShell(ctx, "defaults", "export", d, "-"); err == nil {
					if entries, c, err := macprefs.Collect(d, []byte(out)); err == nil {
						mu.Lock()
						all = append(all, entries...)
						counts.Apply += c.Apply
						counts.Review += c.Review
						counts.Skipped += c.Skipped
						counts.Secret += c.Secret
						mu.Unlock()
					}
				}
				atomic.AddInt64(&done, 1)
			}
		}()
	}
	for _, d := range domains {
		if ctx.Err() != nil {
			break
		}
		workCh <- d
	}
	close(workCh)
	wg.Wait()
	stop()

	// Deterministic, like every other file this tool writes.
	sort.Slice(all, func(i, j int) bool {
		if all[i].Domain != all[j].Domain {
			return all[i].Domain < all[j].Domain
		}
		return all[i].Key < all[j].Key
	})
	return all, counts
}

// writePrefs saves the capture. Owner-only: a preference value can hold a token
// the scanner did not recognise.
func writePrefs(path string, entries []macprefs.Entry, counts macprefs.Counts) error {
	b, err := json.MarshalIndent(prefsFile{Counts: counts, Entries: entries}, "", "  ")
	if err != nil {
		return err
	}
	return sys.WriteFileSecure(path, string(b)+"\n")
}

func readPrefs(path string) (prefsFile, error) {
	var pf prefsFile
	b, err := os.ReadFile(path)
	if err != nil {
		return pf, err
	}
	return pf, json.Unmarshal(b, &pf)
}

// summarisePrefs groups entries by domain, biggest first, for a report that a
// person can actually read — several thousand keys listed one per line is not
// a summary of anything.
func summarisePrefs(entries []macprefs.Entry) []string {
	perDomain := map[string]int{}
	for _, e := range entries {
		if e.Action == "apply" {
			perDomain[e.Domain]++
		}
	}
	type row struct {
		domain string
		n      int
	}
	rows := make([]row, 0, len(perDomain))
	for d, n := range perDomain {
		rows = append(rows, row{d, n})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].n != rows[j].n {
			return rows[i].n > rows[j].n
		}
		return rows[i].domain < rows[j].domain
	})
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, fmt.Sprintf("%s  %s", padTo(r.domain, 46), dim(plural(r.n, "setting"))))
	}
	return out
}

// applyPrefs writes the portable entries back with `defaults write`.
func applyPrefs(ctx context.Context, entries []macprefs.Entry, dryRun, assumeYes, all bool) error {
	// Everything was captured, but only the core domains are written back
	// unless asked otherwise. The rest is overwhelmingly an application's own
	// state, and writing thousands of keys nobody chose is not a migration.
	var selected []macprefs.Entry
	var heldBack int
	for _, e := range entries {
		switch {
		case all || macprefs.IsCore(e.Domain):
			selected = append(selected, e)
		case e.Action == "apply":
			heldBack++
		}
	}

	var todo [][]string
	for _, e := range selected {
		if args := macprefs.WriteArgs(e); args != nil {
			todo = append(todo, args)
		}
	}
	if len(todo) == 0 {
		fmt.Println("No portable preferences to apply.")
		return nil
	}

	fmt.Printf("Will set %s across %s:\n\n",
		plural(len(todo), "preference"), plural(countDomains(selected), "domain"))
	for _, line := range summarisePrefs(selected) {
		fmt.Printf("  %s\n", line)
	}
	if heldBack > 0 {
		fmt.Printf("\n%s\n", dim(fmt.Sprintf(
			"%s in other domains held back (application state). Pass --all to write them too.",
			plural(heldBack, "setting"))))
	}
	if dryRun {
		fmt.Printf("\n%s\n", dim("Dry run — nothing was written."))
		return nil
	}

	fmt.Println()
	if err := confirmWrite(os.Stderr, "Apply these preferences to this Mac?", assumeYes); err != nil {
		return err
	}

	var failed int
	for _, args := range todo {
		if _, err := runShell(ctx, args[0], args[1:]...); err != nil {
			failed++
		}
	}
	fmt.Printf("\n%s Set %s. %s\n", good("✔"), plural(len(todo)-failed, "preference"),
		dim("Log out and back in for everything to take effect."))
	if failed > 0 {
		fmt.Printf("  %s %s could not be written (the app may own the key).\n",
			warn("⚠"), plural(failed, "preference"))
	}
	return nil
}

func countDomains(entries []macprefs.Entry) int {
	seen := map[string]bool{}
	for _, e := range entries {
		if e.Action == "apply" {
			seen[e.Domain] = true
		}
	}
	return len(seen)
}
