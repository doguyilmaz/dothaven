package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	// Dock is the pinned apps, in order: a nested array the per-key pass
	// cannot replay, rebuilt on import from the apps that exist there.
	Dock []string `json:"dock,omitempty"`
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
func writePrefs(path string, entries []macprefs.Entry, counts macprefs.Counts, dock []string) error {
	b, err := encodePrefs(entries, counts, dock)
	if err != nil {
		return err
	}
	return sys.WriteFileSecure(path, string(b))
}

func encodePrefs(entries []macprefs.Entry, counts macprefs.Counts, dock []string) ([]byte, error) {
	// HTML escaping off, like every JSON file dothaven writes: nested values
	// are XML, and \u003c in place of every < makes them unreadable.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(prefsFile{Counts: counts, Entries: entries, Dock: dock}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
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
		case all || macprefs.IsCoreEntry(e.Domain, e.Key):
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
	if notable := notablePrefs(todo); notable != "" {
		fmt.Printf("\n  %s %s\n", dim("including"), notable)
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
	touched := map[string]bool{}
	for _, e := range todo {
		args := macprefs.WriteArgs(e)
		if _, err := runShell(ctx, args[0], args[1:]...); err != nil {
			failed++
			continue
		}
		if e.Type == "plist" && !storedNested(ctx, e) {
			// `defaults` treats a value it cannot parse as a plain string.
			// A language list or shortcut table stored as text is worse than
			// the old value, so the old value goes back.
			undoPref(ctx, e, live[e.Domain+"\x00"+e.Key])
			failed++
			continue
		}
		touched[e.Domain] = true
	}
	restarted := restartForPrefs(ctx, touched)
	note := "Log out and back in for the rest to take effect."
	if len(restarted) == 0 {
		note = "Log out and back in for everything to take effect."
	}
	fmt.Printf("\n%s Set %s. %s\n", good("✔"), plural(len(todo)-failed, "preference"), dim(note))
	if failed > 0 {
		fmt.Printf("  %s %s could not be written (the app may own the key).\n",
			warn("⚠"), plural(failed, "preference"))
	}
	return nil
}

// storedNested reports whether a nested value landed as the array or dict it
// was meant to be.
func storedNested(ctx context.Context, e macprefs.Entry) bool {
	out, err := runShell(ctx, "defaults", "read-type", e.Domain, e.Key)
	if err != nil {
		return false
	}
	return strings.Contains(out, "array") || strings.Contains(out, "dictionary")
}

// undoPref puts back the value a key had before this run — prev is the
// "type\x00value" currentPrefs read — or removes the key if it had none.
func undoPref(ctx context.Context, e macprefs.Entry, prev string) {
	typ, val, ok := strings.Cut(prev, "\x00")
	if !ok {
		_, _ = runShell(ctx, "defaults", "delete", e.Domain, e.Key)
		return
	}
	if args := macprefs.WriteArgs(macprefs.Entry{Domain: e.Domain, Key: e.Key, Type: typ, Value: val}); args != nil {
		_, _ = runShell(ctx, args[0], args[1:]...)
	}
}

// notablePrefs names the nested settings about to be written, which a
// per-domain count would otherwise hide: they are the ones people look for.
func notablePrefs(entries []macprefs.Entry) string {
	var names []string
	apps := 0
	for _, e := range entries {
		if e.Type != "plist" {
			continue
		}
		switch {
		case e.Key == "NSUserKeyEquivalents":
			apps++
		case e.Key == "AppleLanguages":
			names = append(names, "language order")
		case e.Key == "AppleEnabledInputSources":
			names = append(names, "keyboard layouts")
		case e.Key == "AppleSymbolicHotKeys":
			names = append(names, "keyboard shortcuts")
		}
	}
	if apps > 0 {
		names = append(names, "App Shortcuts for "+plural(apps, "app"))
	}
	return strings.Join(names, ", ")
}

// activateSettings makes a changed shortcut table take effect in the running
// session instead of at the next log-in.
const activateSettings = "/System/Library/PrivateFrameworks/SystemAdministration.framework/Resources/activateSettings"

// restartForPrefs restarts the parts of the UI that only read their settings at
// launch, so the change shows now rather than after a log-out. Each is
// relaunched by macOS immediately; Finder closes its windows, nothing else.
func restartForPrefs(ctx context.Context, touched map[string]bool) []string {
	var procs []string
	if touched["com.apple.dock"] || touched["com.apple.spaces"] {
		procs = append(procs, "Dock")
	}
	if touched["com.apple.finder"] || touched["com.apple.desktopservices"] {
		procs = append(procs, "Finder")
	}
	if touched["com.apple.controlcenter"] || touched["com.apple.menuextra.clock"] || touched["com.apple.screencapture"] {
		procs = append(procs, "SystemUIServer")
	}
	var done []string
	for _, p := range procs {
		if _, err := runShell(ctx, "killall", p); err == nil {
			done = append(done, p)
		}
	}
	if len(done) > 0 {
		fmt.Printf("  %s restarted %s to pick the changes up\n", dim("•"), strings.Join(done, ", "))
	}
	if touched["com.apple.symbolichotkeys"] {
		if _, err := os.Stat(activateSettings); err == nil {
			if _, err := runShell(ctx, activateSettings, "-u"); err == nil {
				fmt.Printf("  %s reloaded keyboard shortcuts\n", dim("•"))
				done = append(done, "shortcuts")
			}
		}
	}
	return done
}

// captureDock reads the Dock's pinned apps.
func captureDock(ctx context.Context) []string {
	out, err := runShell(ctx, "defaults", "read", "com.apple.dock", "persistent-apps")
	if err != nil {
		return nil
	}
	return macprefs.ParseDockApps(out)
}

// applyDock rebuilds the Dock from the old machine's app list, keeping only
// apps installed here — a tile for a missing app is a question mark.
func applyDock(ctx context.Context, apps []string, dryRun, assumeYes bool) error {
	var have, missing []string
	for _, a := range apps {
		if fi, err := os.Stat(a); err == nil && fi.IsDir() {
			have = append(have, a)
		} else {
			missing = append(missing, strings.TrimSuffix(filepath.Base(a), ".app"))
		}
	}
	current := captureDock(ctx)
	if strings.Join(current, "\x00") == strings.Join(have, "\x00") && len(have) > 0 {
		fmt.Printf("%s Your Dock already has the same %s.\n", good("✓"), plural(len(have), "app"))
		return nil
	}
	fmt.Printf("Dock: %s from the old machine", plural(len(apps), "app"))
	if len(have) > 0 {
		fmt.Printf(", %d installed here", len(have))
	}
	fmt.Println(":")
	names := make([]string, len(have))
	for i, a := range have {
		names[i] = strings.TrimSuffix(filepath.Base(a), ".app")
	}
	if len(names) > 0 {
		fmt.Printf("  %s\n", strings.Join(names, " · "))
	}
	if len(missing) > 0 {
		fmt.Printf("  %s %s\n", dim("not installed yet (reinstall first, then import again):"), dim(strings.Join(missing, ", ")))
	}
	if len(have) == 0 || dryRun {
		if dryRun {
			fmt.Println(dim("  Dry run — the Dock was not changed."))
		}
		return nil
	}
	if err := confirmWrite(os.Stderr, fmt.Sprintf("Replace your Dock's apps with these %d?", len(have)), assumeYes); err != nil {
		return err
	}
	if _, err := runShell(ctx, "defaults", "write", "com.apple.dock", "persistent-apps", "-array"); err != nil {
		return fmt.Errorf("could not clear the Dock: %w", err)
	}
	for _, a := range have {
		if _, err := runShell(ctx, "defaults", "write", "com.apple.dock", "persistent-apps", "-array-add", macprefs.DockTile(a)); err != nil {
			fmt.Fprintf(os.Stderr, "  %s %s: %v\n", warn("⚠"), filepath.Base(a), err)
		}
	}
	restartForPrefs(ctx, map[string]bool{"com.apple.dock": true})
	fmt.Printf("%s Dock rebuilt with %s.\n", good("✔"), plural(len(have), "app"))
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
