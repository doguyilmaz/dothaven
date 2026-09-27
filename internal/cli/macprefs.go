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
	"github.com/doguyilmaz/dothaven/internal/tui"
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
	// No domains means no `defaults` tool — this is not a Mac. Returning here
	// keeps a progress bar and eight goroutines from being started for nothing.
	if len(domains) == 0 {
		return nil, macprefs.Counts{}
	}
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
	b, err := encodePrefs(entries, counts)
	if err != nil {
		return err
	}
	return sys.WriteFileSecure(path, string(b))
}

func encodePrefs(entries []macprefs.Entry, counts macprefs.Counts) ([]byte, error) {
	b, err := json.MarshalIndent(prefsFile{Counts: counts, Entries: entries}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
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

// currentPrefs reads this Mac's live value of every key in the given domains,
// as "type\x00value" by "domain\x00key", so settings already in place can be
// shown as done instead of offered again.
func currentPrefs(ctx context.Context, domains []string) map[string]string {
	var (
		mu  sync.Mutex
		out = map[string]string{}
		ch  = make(chan string)
		wg  sync.WaitGroup
	)
	for range min(prefsWorkers, max(1, len(domains))) {
		wg.Go(func() {
			for d := range ch {
				raw, err := runShell(ctx, "defaults", "export", d, "-")
				if err != nil {
					continue
				}
				entries, _, err := macprefs.Collect(d, []byte(raw))
				if err != nil {
					continue
				}
				mu.Lock()
				for _, e := range entries {
					out[e.Domain+"\x00"+e.Key] = e.Type + "\x00" + e.Value
				}
				mu.Unlock()
			}
		})
	}
	for _, d := range domains {
		if ctx.Err() != nil {
			break
		}
		ch <- d
	}
	close(ch)
	wg.Wait()
	return out
}

// applyPrefs writes the portable entries back with `defaults write` — only
// the ones not already set, and on a terminal only the domains you pick.
func applyPrefs(ctx context.Context, entries []macprefs.Entry, dryRun, assumeYes, all bool) error {
	// Everything was captured, but only the core domains are written back
	// unless asked otherwise. The rest is overwhelmingly an application's own
	// state, and writing thousands of keys nobody chose is not a migration.
	var selected []macprefs.Entry
	var heldBack int
	for _, e := range entries {
		switch {
		case all || macprefs.IsCore(e.Domain):
			if macprefs.WriteArgs(e) != nil {
				selected = append(selected, e)
			}
		case e.Action == "apply":
			heldBack++
		}
	}
	if len(selected) == 0 {
		fmt.Println("No portable preferences to apply.")
		return nil
	}

	// Already set is already applied: compare with what this Mac has now.
	domains := uniqueDomains(selected)
	live := currentPrefs(ctx, domains)
	var todo []macprefs.Entry
	already := 0
	for _, e := range selected {
		if live[e.Domain+"\x00"+e.Key] == e.Type+"\x00"+e.Value {
			already++
			continue
		}
		todo = append(todo, e)
	}
	if already > 0 {
		fmt.Printf("%s %s already set on this Mac.\n", good("✓"), plural(already, "setting"))
	}
	if len(todo) == 0 {
		fmt.Println(good("✓ Nothing to change — your settings are already in place."))
		printPrefsReview(entries, all)
		return nil
	}

	fmt.Printf("Will set %s across %s:\n\n", plural(len(todo), "preference"), plural(countDomains(todo), "domain"))
	for _, line := range summarisePrefs(todo) {
		fmt.Printf("  %s\n", line)
	}
	if heldBack > 0 {
		fmt.Printf("\n%s\n", dim(fmt.Sprintf(
			"%s in other domains held back (application state). Pass --all to write them too.",
			plural(heldBack, "setting"))))
	}
	printPrefsReview(entries, all)
	if dryRun {
		fmt.Printf("\n%s\n", dim("Dry run — nothing was written."))
		return nil
	}

	fmt.Println()
	if tui.Interactive() && !assumeYes {
		counts := map[string]int{}
		for _, e := range todo {
			counts[e.Domain]++
		}
		var items []tui.PickItem
		for _, d := range uniqueDomains(todo) {
			items = append(items, tui.PickItem{Label: d, Value: d, Hint: plural(counts[d], "setting"), Selected: true})
		}
		picked, err := tui.MultiPick("Which settings should be applied?", "space toggles · enter applies", items)
		if err != nil {
			return ignoreAbort(err)
		}
		keep := map[string]bool{}
		for _, p := range picked {
			keep[p] = true
		}
		var chosen []macprefs.Entry
		for _, e := range todo {
			if keep[e.Domain] {
				chosen = append(chosen, e)
			}
		}
		if len(chosen) == 0 {
			fmt.Println("Nothing selected.")
			return nil
		}
		todo = chosen
	} else if err := confirmWrite(os.Stderr, "Apply these preferences to this Mac?", assumeYes); err != nil {
		return err
	}

	var failed int
	for _, e := range todo {
		args := macprefs.WriteArgs(e)
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

func uniqueDomains(entries []macprefs.Entry) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range entries {
		if !seen[e.Domain] {
			seen[e.Domain] = true
			out = append(out, e.Domain)
		}
	}
	sort.Strings(out)
	return out
}

// printPrefsReview lists the settings that were captured and will never be
// written: their value names the machine they came from — a path into someone
// else's home directory, an identifier for a display that is not here.
//
// They are shown because a value nobody looks at is a value nobody can act on.
// Captured and then silently dropped is the same as not captured, except it
// takes up space and gives a false impression of completeness.
func printPrefsReview(entries []macprefs.Entry, all bool) {
	var core, other []macprefs.Entry
	for _, e := range entries {
		if e.Action != "review" {
			continue
		}
		if all || macprefs.IsCore(e.Domain) {
			core = append(core, e)
		} else {
			other = append(other, e)
		}
	}
	if len(core) == 0 && len(other) == 0 {
		return
	}

	fmt.Printf("\n%s %s carried a value from the old machine — set these by hand:\n",
		warn("⚠"), plural(len(core)+len(other), "setting"))
	for _, e := range core {
		fmt.Printf("  %s %s\n", padTo(e.Domain+" "+e.Key, 46), dim(ellipsize(e.Value, 30)))
	}
	if len(other) > 0 {
		fmt.Printf("  %s\n", dim(fmt.Sprintf("…and %d more in non-core domains.", len(other))))
	}
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
