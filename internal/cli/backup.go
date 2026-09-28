package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/doguyilmaz/dothaven/internal/backup"
	"github.com/doguyilmaz/dothaven/internal/chezmoi"
	"github.com/doguyilmaz/dothaven/internal/collect"
	"github.com/doguyilmaz/dothaven/internal/registry"
	"github.com/doguyilmaz/dothaven/internal/scan"
	"github.com/doguyilmaz/dothaven/internal/sys"
	"github.com/doguyilmaz/dothaven/internal/tui"
	"github.com/spf13/cobra"
)

// Pseudo-categories: parts of a backup that are not registry files, selected
// with the same --only/--skip as everything else.
const (
	catInventory = "inventory" // installed apps & packages, and how to reinstall them
	catMacOS     = "macos"     // system settings held by cfprefsd
)

// categoryAbout says, in words a person picking from a list would use, what
// each category holds.
var categoryAbout = map[string]string{
	"ai":         "Claude, Codex, Cursor, Gemini… skills, agents, MCP, plugins",
	"shell":      "zsh, bash, fish and their frameworks",
	"git":        "git config, global hooks and ignores, gh/glab",
	"editor":     "VS Code, Cursor, Zed, Neovim, Vim, Helix…",
	"terminal":   "tmux, Ghostty, kitty, WezTerm, Starship…",
	"ssh":        "ssh config, known_hosts and keys",
	"cloud":      "AWS, GCP, Azure, kubectl, Docker, Vercel…",
	"devops":     "helm, k9s, ansible, terraform…",
	"lang":       "Ruby, Python, Rust, Go, PHP, .NET, JS tool config",
	"npm":        "npm config",
	"bun":        "bun config",
	"db":         "database client config and saved passwords",
	"secrets":    ".netrc, Vault token, GnuPG and age keys",
	"vm":         "version pins (.tool-versions, mise, .nvmrc)",
	"net":        "curl and wget defaults",
	"dev":        "direnv, chezmoi config",
	"apps":       "Karabiner, Hammerspoon, window managers…",
	"schedule":   "launchd agents",
	"build":      "Maven and Gradle settings",
	"mobile":     "Xcode user data, Android keystores",
	"dothaven":   "dothaven's own settings",
	"fonts":      "fonts you installed yourself (~/Library/Fonts)",
	"extra":      "paths you added with `dothaven include`",
	catInventory: "list of installed apps & packages, to reinstall",
	catMacOS:     "system settings: trackpad, keyboard, Dock, Finder…",
}

// backupGroups aggregates backup targets into selectable category groups,
// noting the ones that hold credentials.
func backupGroups(targets []registry.BackupTarget, credNote string) []tui.Group {
	creds := map[string]bool{}
	seen := map[string]bool{}
	var cats []string
	for _, t := range targets {
		if !seen[t.Category] {
			seen[t.Category] = true
			cats = append(cats, t.Category)
		}
		if t.Sensitivity == registry.High {
			creds[t.Category] = true
		}
	}
	sort.Strings(cats)
	groups := make([]tui.Group, 0, len(cats))
	for _, c := range cats {
		g := tui.Group{Name: c, About: categoryAbout[c]}
		if creds[c] {
			g.Note = credNote
		}
		groups = append(groups, g)
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

// backupOpts is everything that decides what a backup contains and where it
// goes. The flags and the menu both fill one in, so they cannot drift.
type backupOpts struct {
	output     string
	archive    bool
	encrypt    bool
	noRedact   bool
	split      bool               // plain files plus an encrypted bundle of the sensitive ones
	digest     *backup.DigestSink // when set, fingerprints the content as it is written
	only, skip []string
	passphrase string // already asked (the menu asks before the slow part)
	remote     bool   // the result leaves this machine (a GitHub push)
}

func (o backupOpts) redact() bool { return !o.noRedact && !o.encrypt }

// backupOutcome is what a finished backup reports.
type backupOutcome struct {
	path      string
	res       backup.Result
	inventory bool
	prefs     int
	encrypted bool
	redacted  bool
	size      int64
}

func newBackupCmd(env *sys.OS) *cobra.Command {
	var o backupOpts
	c := &cobra.Command{
		Use:   "backup",
		Short: "Save your config — a folder here, or one encrypted file to carry",
		Long: "Copies every config dothaven tracks (plus anything you added with `include`),\n" +
			"a list of your installed apps and packages, and your macOS settings.\n\n" +
			"  dothaven backup             a folder on this machine. Secrets are redacted and\n" +
			"                              credential files (SSH keys, cloud logins) left out.\n" +
			"  dothaven backup --encrypt   ONE age-encrypted file with everything, keys and\n" +
			"                              tokens included. This is the one for a new machine.\n\n" +
			"Nothing is written in plaintext when encrypting — not even temporarily.\n" +
			"Restore either kind with `dothaven restore <path>`.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if o.encrypt {
				o.archive = true
			}
			if len(o.only) == 0 && len(o.skip) == 0 && tui.Interactive() {
				chosen, err := pickBackupCategories(env, o.encrypt)
				if err != nil || chosen == nil {
					return err
				}
				o.only = chosen
				added, err := reviewUncovered(env, false)
				if err != nil {
					return err
				}
				// The categories were picked before these paths existed as
				// includes; "in this backup" has to mean this one too.
				if added > 0 && !contains(o.only, registry.ExtraCategory) {
					o.only = append(o.only, registry.ExtraCategory)
				}
			}
			out, err := runBackup(cmd.Context(), cmd, env, o)
			if err != nil {
				return err
			}
			printBackupOutcome(env, out)
			return nil
		},
	}
	c.Flags().BoolVar(&o.encrypt, "encrypt", false, "one age-encrypted file with everything, credentials included (asks for a passphrase)")
	c.Flags().BoolVar(&o.archive, "archive", false, "one .tar.gz file instead of a folder (still redacted, not encrypted)")
	c.Flags().BoolVar(&o.noRedact, "no-redact", false, "keep raw secret values in a plaintext backup (prefer --encrypt)")
	c.Flags().StringVarP(&o.output, "output", "o", "", "where to write it, e.g. a USB drive (default: ~/.local/share/dothaven)")
	c.Flags().StringSliceVar(&o.only, "only", nil, "only these categories, comma-separated (listed in the help above)")
	c.Flags().StringSliceVar(&o.skip, "skip", nil, "skip these categories, e.g. --skip inventory,macos")
	return c
}

// pickBackupCategories shows the category picker. A nil result with no error
// means the user backed out or picked nothing.
func pickBackupCategories(env *sys.OS, encrypt bool) ([]string, error) {
	note := "🔑 credentials — left out unless --encrypt"
	if encrypt {
		note = "🔑 credentials"
	}
	groups := backupGroups(registry.BackupTargets(env.Home(), allEntries(env)), note)
	groups = append(groups, tui.Group{Name: catInventory, About: categoryAbout[catInventory]})
	if runtime.GOOS == "darwin" {
		groups = append(groups, tui.Group{Name: catMacOS, About: categoryAbout[catMacOS]})
	}
	chosen, err := tui.SelectCategories("What to back up", groups)
	if errors.Is(err, tui.ErrAborted) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(chosen) == 0 {
		fmt.Println("Nothing selected.")
		return nil, nil
	}
	return chosen, nil
}

// reviewUncovered offers the paths nothing covers, once. Picked ones join the
// include list; the rest are remembered as declined so the question is not
// asked on every backup. force re-asks about declined paths too.
func reviewUncovered(env *sys.OS, force bool) (int, error) {
	inc := loadIncludes(env)
	if force {
		inc.Declined = nil
	}
	pending := collectUncovered(env, inc)
	if len(pending) == 0 {
		if force {
			fmt.Println(good("✓ Everything that looks like config is covered."))
		}
		return 0, nil
	}
	if !tui.Interactive() {
		return 0, nil
	}
	picked, err := tui.PickSome(
		fmt.Sprintf("%d things in your home folder aren't in any backup yet — include some?", len(pending)),
		"space picks · enter continues. Whatever you leave unpicked won't be asked about again\n"+
			"(change your mind any time: dothaven include <path>).",
		pending)
	if errors.Is(err, tui.ErrAborted) {
		return 0, nil // skip the question this time; ask again next backup
	}
	if err != nil {
		return 0, err
	}
	for _, p := range pending {
		if contains(picked, p) {
			inc.Paths = append(inc.Paths, p)
		} else {
			inc.Declined = append(inc.Declined, p)
		}
	}
	if err := saveIncludes(env, inc); err != nil {
		return 0, err
	}
	if len(picked) > 0 {
		fmt.Printf("%s %s added — they will be in this and every later backup.\n", good("+"), plural(len(picked), "path"))
	}
	return len(picked), nil
}

func collectUncovered(env *sys.OS, inc registry.Includes) []string {
	// What the git config points at is carried already; not offered again.
	inc.Paths = append(append([]string(nil), inc.Paths...), gitReferenced(env)...)
	return collect.Uncovered(env.ListDir, env.Home(), registry.Entries, inc)
}

// validateCategories rejects a --only/--skip name that is no category. A typo
// used to select nothing and report "no files found", which reads as "you have
// no config" rather than "you misspelled it".
func validateCategories(targets []registry.BackupTarget, only, skip []string, extra ...string) error {
	known := map[string]bool{}
	for _, t := range targets {
		known[t.Category] = true
	}
	// A category is a name, not a promise that this OS has one: --skip
	// schedule must not fail on Linux because launchd agents are macOS-only.
	for _, e := range registry.Entries {
		known[e.Category] = true
	}
	for _, e := range extra {
		known[e] = true
	}
	for _, c := range append(append([]string(nil), only...), skip...) {
		if !known[c] {
			names := make([]string, 0, len(known))
			for k := range known {
				names = append(names, k)
			}
			sort.Strings(names)
			return fmt.Errorf("unknown category %q — choose from: %s", c, strings.Join(names, ", "))
		}
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// runBackup writes one backup and reports what went in.
func runBackup(ctx context.Context, cmd *cobra.Command, env *sys.OS, o backupOpts) (backupOutcome, error) {
	// Encrypted means one encrypted file: a folder written with the gate off
	// would be plaintext with nothing redacted.
	if o.encrypt {
		o.archive = true
	}
	redact := o.redact()
	out := backupOutcome{encrypted: o.encrypt, redacted: redact}

	if o.encrypt && o.passphrase == "" {
		p, err := newPassphrase()
		if err != nil {
			return out, err
		}
		o.passphrase = p
	}

	dir := o.output
	if dir == "" {
		dir = env.DataDir()
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return out, err
	}
	host := hostname()
	name := fmt.Sprintf("backup-%s-%s", host, sys.Timestamp(time.Now()))
	targets := registry.BackupTargets(env.Home(), allEntries(env))
	if err := validateCategories(targets, o.only, o.skip, catInventory, catMacOS, registry.ExtraCategory); err != nil {
		return out, err
	}

	fill := func(sink backup.Sink) error { return fillBackup(ctx, cmd, env, o, targets, sink, host, &out) }

	var err error
	if o.archive {
		out.path = filepath.Join(dir, name+".tar.gz")
		if o.encrypt {
			out.path += ".age"
		}
		if o.encrypt {
			err = backup.WriteEncryptedArchive(out.path, name, o.passphrase, fill)
		} else {
			err = backup.WriteArchive(out.path, name, fill)
		}
	} else {
		out.path = filepath.Join(dir, name)
		err = fill(backup.DirSink{Root: out.path})
		if err != nil {
			_ = os.RemoveAll(out.path) // a half-written folder must not pass for a backup
		}
	}
	switch {
	case errors.Is(err, backup.ErrNothingToWrite):
		return out, fmt.Errorf("nothing to back up — no tracked files found for this selection")
	case errors.Is(err, context.Canceled):
		fmt.Fprintln(os.Stderr, "Backup cancelled — nothing was kept.")
		return out, ExitError{Code: 130}
	case err != nil:
		return out, err
	}
	if fi, err := os.Stat(out.path); err == nil && !fi.IsDir() {
		out.size = fi.Size()
	} else {
		out.size = out.res.TotalBytes
	}
	return out, nil
}

// fillBackup writes one backup's contents into sink: the tracked files, the
// inventory, macOS settings and the MANIFEST. Shared by every kind of backup —
// folder, archive, encrypted, and the split form a GitHub push uses.
func fillBackup(ctx context.Context, cmd *cobra.Command, env *sys.OS, o backupOpts, targets []registry.BackupTarget, sink backup.Sink, host string, out *backupOutcome) error {
	if o.remote {
		sink = backup.AgeMaskSink{Inner: sink}
	}
	if o.digest != nil {
		o.digest.Inner = sink
		o.digest.Exclude = map[string]bool{"MANIFEST.txt": true}
		sink = o.digest
	}
	redact := o.redact()
	var read int64
	var at atomic.Pointer[string]
	stop := startActivity("Reading your config", &read, 0, func() string {
		if p := at.Load(); p != nil {
			return *p
		}
		return ""
	})
	res, err := backup.RunTo(targets, sink, backup.Options{
		Context: ctx, Redact: redact, Encrypted: o.encrypt, Only: o.only, Skip: o.skip, Remote: o.remote,
		// A readable push cannot hold .git folders, so it does not read them.
		SkipVCS: o.remote && !o.encrypt,
		Reading: func(dest string, file bool) {
			at.Store(&dest)
			if file {
				atomic.AddInt64(&read, 1)
			}
			runlog.reading(dest, file)
		},
	})
	stop()
	runlog.done()
	runlog.stepf("read %d files (%s)", res.TotalFiles, humanBytes(res.TotalBytes))
	out.res = res
	if err != nil {
		return err
	}
	if registry.Selected(catInventory, o.only, o.skip) {
		ok, err := writeInventory(ctx, env, sink, redact)
		if err != nil {
			return err
		}
		out.inventory = ok
	}
	// The Mac's own settings — scroll direction, key repeat, Dock size,
	// Finder options — are held by cfprefsd, not by any file the walk
	// above reads. A backup without them restores a machine that has all
	// your config and still feels wrong.
	if runtime.GOOS == "darwin" && registry.Selected(catMacOS, o.only, o.skip) {
		n, err := writePrefsTo(ctx, sink)
		if err != nil {
			return err
		}
		out.prefs = n
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if out.res.TotalFiles == 0 && !out.inventory && out.prefs == 0 {
		return backup.ErrNothingToWrite
	}
	manifest := backup.Manifest(backup.ManifestMeta{
		Host: host, Home: env.Home(), OS: runtime.GOOS, Version: cmd.Root().Version,
		Created: time.Now().Format(time.RFC3339), Redacted: redact, Encrypted: o.encrypt, Split: o.split,
	}, out.res)
	return sink.Add("MANIFEST.txt", []byte(manifest), false)
}

// writeInventory records what is installed — the half of a machine no config
// file describes — together with a script that reinstalls it. Without this a
// restored machine has every dotfile and none of the programs they configure.
func writeInventory(ctx context.Context, env *sys.OS, sink backup.Sink, redact bool) (bool, error) {
	snap := gatherInventory(ctx, env)
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if redact {
		for name, sec := range snap {
			if kept, _ := scan.RedactSection(name, &sec); kept {
				snap[name] = sec
			} else {
				delete(snap, name)
			}
		}
	}
	if len(snap) <= 1 { // only meta: nothing installed that we can see
		return false, nil
	}
	data, err := snap.Serialize()
	if err != nil {
		return false, err
	}
	if err := sink.Add("inventory/snapshot.json", data, false); err != nil {
		return false, err
	}
	m := manifestFromSnapshot(snap, false)
	if m.Brewfile != "" {
		if err := sink.Add("inventory/Brewfile", []byte(m.Brewfile+"\n"), false); err != nil {
			return false, err
		}
	}
	if script, ok := chezmoi.BuildPackageInstallScript(m); ok {
		if err := sink.Add("inventory/install-packages.sh", []byte(script), true); err != nil {
			return false, err
		}
	}
	if tab, err := runShell(ctx, "crontab", "-l"); err == nil && strings.TrimSpace(tab) != "" {
		sr := scan.ScanContentFull("crontab", tab)
		if !redact || sr.Action != scan.Skip {
			if redact {
				tab = scan.ApplyRedactions(tab, sr)
			}
			if err := sink.Add("inventory/crontab", []byte(tab+"\n"), false); err != nil {
				return false, err
			}
		}
	}
	return true, nil
}

// writePrefsTo captures macOS preferences into the backup. Returns how many
// settings would be applied on restore.
func writePrefsTo(ctx context.Context, sink backup.Sink) (int, error) {
	entries, counts := capturePrefs(ctx, listPrefDomains(ctx))
	if len(entries) == 0 {
		return 0, nil
	}
	data, err := encodePrefs(entries, counts, captureDock(ctx))
	if err != nil {
		return 0, err
	}
	// Preference values are whatever apps chose to store. In a split backup
	// they go to the encrypted part: the scanner reads prefs.json as JSON,
	// where a key and its value sit on separate lines.
	if cs, ok := sink.(backup.ClassifyingSink); ok {
		return counts.Apply, cs.AddClassified("macos-defaults/"+prefsFileName, data, false, true)
	}
	return counts.Apply, sink.Add("macos-defaults/"+prefsFileName, data, false)
}

func hostname() string {
	h, _ := os.Hostname()
	if h == "" {
		return "machine"
	}
	// A hostname like "Dogus-MacBook-Pro.local" makes a long name; the domain
	// adds nothing a person needs to tell two backups apart.
	return strings.TrimSuffix(h, ".local")
}

func printBackupOutcome(env *sys.OS, o backupOutcome) {
	res := o.res
	kind := "Backup saved"
	switch {
	case o.encrypted:
		kind = "Encrypted backup saved"
	case !o.redacted:
		kind = "Backup saved (raw values, NOT encrypted)"
	}
	fmt.Printf("\n%s %s\n", good("✓"), bold(fmt.Sprintf("%s — %s, %s", kind, plural(res.TotalFiles, "file"), humanBytes(o.size))))
	fmt.Printf("  %s\n", o.path)
	if len(res.PerCategory) > 0 {
		fmt.Printf("  %s\n", dim(formatCategories(res.PerCategory)))
	}
	var extras []string
	if o.inventory {
		extras = append(extras, "installed apps & packages list")
	}
	if o.prefs > 0 {
		extras = append(extras, plural(o.prefs, "macOS setting"))
	}
	if len(extras) > 0 {
		fmt.Printf("  %s\n", dim("+ "+strings.Join(extras, ", ")))
	}

	printLeftOut(res, o.encrypted, "dothaven backup --encrypt")
	if o.redacted {
		if report := scan.FormatReport(scan.Summarize(res.ScanResults), scan.ReportOptions{Color: colorOn()}); strings.TrimSpace(report) != "" {
			fmt.Println(report)
			fmt.Println(dim("  Redacted files are kept for reference but not restored over your real ones."))
		}
	}
	if u := uncovered(env); len(u) > 0 {
		fmt.Printf("\n%s %s — %s\n", dim("?"), dim(plural(len(u), "path")+" nothing covers yet"), kbd("dothaven include --list"))
	}

	fmt.Println("\n" + bold("Next:"))
	if o.encrypted || strings.HasSuffix(o.path, ".tar.gz") {
		fmt.Println("  Copy this file off this machine — a USB drive, cloud storage, another computer.")
		fmt.Println("  It lives on the disk you are about to replace.")
		fmt.Printf("  On the new machine: %s\n", kbd("dothaven restore "+filepath.Base(o.path)))
		if o.encrypted {
			fmt.Println(dim("  You will need the passphrase. Nothing can open this file without it."))
		}
		return
	}
	fmt.Printf("  %s             %s\n", kbd("dothaven status"), dim("what changed since this backup"))
	fmt.Printf("  %s   %s\n", kbd("dothaven backup --encrypt"), dim("one complete encrypted file for a new machine"))
}

// printLeftOut lists everything that exists and did not go in, loudest first.
// A backup is judged by what it is missing, and nobody reads a MANIFEST until
// too late. complete is the command that would carry the credentials.
func printLeftOut(res backup.Result, encrypted bool, complete string) {
	if len(res.SkippedSensitive)+len(res.Withheld) > 0 {
		fmt.Printf("\n%s %s\n", warn("⚠"), bold(fmt.Sprintf("%s with credentials left out of this plaintext backup:",
			plural(len(res.SkippedSensitive)+len(res.Withheld), "path"))))
		printList(append(append([]string(nil), res.SkippedSensitive...), res.Withheld...), 8)
		fmt.Printf("  For a complete copy, keys included: %s\n", kbd(complete))
	}
	if len(res.KeptLocal) > 0 {
		fmt.Printf("\n%s %s\n", warn("⚠"), bold(fmt.Sprintf("%s kept off GitHub, even encrypted:", plural(len(res.KeptLocal), "age key file"))))
		printList(res.KeptLocal, 8)
		fmt.Printf("  They open the encrypted files in your dotfiles repo, so they never go into a repository.\n")
		fmt.Printf("  Carry them to the new machine with %s, or keep them in your password manager.\n", kbd("dothaven backup --encrypt"))
	}
	if len(res.TooLarge) > 0 {
		fmt.Printf("\n%s %s\n", warn("⚠"), bold(fmt.Sprintf("%s left out for size:", plural(len(res.TooLarge), "file"))))
		var lines []string
		for _, t := range res.TooLarge {
			lines = append(lines, fmt.Sprintf("%s  %s", t.Dest, dim(fmt.Sprintf("%s, %s", humanBytes(t.Size), t.Reason))))
		}
		printList(lines, 8)
	}
	if len(res.ReadErrors) > 0 {
		fmt.Fprintf(os.Stderr, "\n%s %s\n", danger("✗"), bold(fmt.Sprintf("%s exist but could not be read:", plural(len(res.ReadErrors), "file"))))
		printListTo(os.Stderr, res.ReadErrors, 8)
	}
	if len(res.RawSecrets) > 0 && !encrypted {
		fmt.Fprintf(os.Stderr, "\n%s %s\n", danger("🔴"), bold(fmt.Sprintf("%s holding private keys were written UNENCRYPTED:", plural(len(res.RawSecrets), "file"))))
		printListTo(os.Stderr, res.RawSecrets, 8)
		fmt.Fprintf(os.Stderr, "  Treat this backup as secret, or use %s instead.\n", kbd("dothaven backup --encrypt"))
	}
}

func printList(items []string, limit int) { printListTo(os.Stdout, items, limit) }

func printListTo(w *os.File, items []string, limit int) {
	sorted := append([]string(nil), items...)
	sort.Strings(sorted)
	for i, it := range sorted {
		if i == limit {
			fmt.Fprintf(w, "    %s\n", dim(fmt.Sprintf("…and %d more (listed in MANIFEST.txt)", len(sorted)-limit)))
			break
		}
		fmt.Fprintf(w, "    %s\n", it)
	}
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
