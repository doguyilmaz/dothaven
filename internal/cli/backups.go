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
	"sync"
	"time"

	"github.com/doguyilmaz/dothaven/internal/backup"
	"github.com/doguyilmaz/dothaven/internal/sys"
	"github.com/doguyilmaz/dothaven/internal/tui"
)

// latestBackup returns the most recently modified backup-* directory in dir, or
// "" if none. Sorting by mtime (not name) is correct when a dir holds backups
// from several machines: a name sort orders by host before timestamp, so it
// would pick the alphabetically-last host's backup rather than the newest.
// Archives are ignored — status and diff compare a readable tree.
func latestBackup(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var newest string
	var newestMod time.Time
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "backup-") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if newest == "" || info.ModTime().After(newestMod) {
			newest, newestMod = e.Name(), info.ModTime()
		}
	}
	if newest == "" {
		return ""
	}
	return filepath.Join(dir, newest)
}

// newestBackup is the most recent backup of any kind — folder, archive or
// encrypted file — in any place findBackups looks.
func newestBackup(env *sys.OS) string {
	if found := findBackups(env); len(found) > 0 {
		return found[0].Path
	}
	return ""
}

// backupAge renders a backup directory's mtime as a coarse "Xm/Xh/Xd ago".
func backupAge(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "unknown"
	}
	mins := int(time.Since(info.ModTime()).Minutes())
	if mins < 60 {
		return fmt.Sprintf("%dm ago", mins)
	}
	if hours := mins / 60; hours < 24 {
		return fmt.Sprintf("%dh ago", hours)
	}
	return fmt.Sprintf("%dd ago", mins/60/24)
}

// foundBackup is a backup discovered on disk.
type foundBackup struct {
	Path string
	Mod  time.Time
	Size int64
	Kind string // "folder" | "archive" | "encrypted"
	real string // resolved path, so a backup reached twice is listed once
}

// readDirTimeout is os.ReadDir that gives up after d. A disconnected network
// share under /Volumes can block a directory read for minutes; looking for
// backups must never be the thing that hangs.
func readDirTimeout(dir string, d time.Duration) []os.DirEntry {
	if recentlyStalled(dir) {
		return nil
	}
	ch := make(chan []os.DirEntry, 1) // buffered: a late reader never blocks
	go func() {
		e, _ := os.ReadDir(dir)
		ch <- e
	}()
	select {
	case e := <-ch:
		return e
	case <-time.After(d):
		markStalled(dir)
		return nil
	}
}

// stalled remembers folders that did not answer in time — a network share
// that went away, a disk spinning up. The goroutine stuck on one cannot be
// stopped, so the next few minutes do not start another: the dashboard asks
// every 20 seconds, and they would pile up.
var stalled sync.Map // dir → time.Time

func markStalled(dir string) { stalled.Store(dir, time.Now()) }

func recentlyStalled(dir string) bool {
	t, ok := stalled.Load(dir)
	return ok && time.Since(t.(time.Time)) < 5*time.Minute
}

// backupSearchDirs are where a backup plausibly is on a machine you are
// restoring onto: dothaven's own folder, the usual drop spots, and the root of
// every mounted drive.
func backupSearchDirs(env *sys.OS) []string {
	home := env.Home()
	dirs := []string{env.DataDir(), cwd(), filepath.Join(home, "Downloads"), filepath.Join(home, "Desktop"), filepath.Join(home, "Documents")}
	var mounts []string
	switch runtime.GOOS {
	case "darwin":
		mounts = []string{"/Volumes"}
	case "linux":
		user := filepath.Base(home)
		mounts = []string{filepath.Join("/media", user), filepath.Join("/run/media", user), "/mnt"}
	}
	for _, m := range mounts {
		for _, e := range readDirTimeout(m, time.Second) {
			if e.Name() == "Macintosh HD" || !e.IsDir() && e.Type()&os.ModeSymlink == 0 {
				continue
			}
			vol := filepath.Join(m, e.Name())
			dirs = append(dirs, vol)
			// One level down too: people make a folder for it.
			for _, sub := range readDirTimeout(vol, time.Second) {
				if sub.IsDir() && !strings.HasPrefix(sub.Name(), ".") {
					dirs = append(dirs, filepath.Join(vol, sub.Name()))
				}
			}
		}
	}
	return dirs
}

// looksLikeBackupName is the naming check; content decides the kind.
func looksLikeBackupName(name string) bool {
	return strings.HasPrefix(name, "backup-") || strings.HasPrefix(name, "dothaven-")
}

// findBackups lists every backup it can find, newest first. Every folder is
// looked at in parallel under one deadline, listing and file checks alike: a
// stalled drive costs at most that long, and only its own results.
func findBackups(env *sys.OS) []foundBackup {
	dirs := backupSearchDirs(env)
	results := make([][]foundBackup, len(dirs))
	done := make([]bool, len(dirs))
	type result struct {
		i     int
		found []foundBackup
	}
	ch := make(chan result, len(dirs)) // buffered: a late folder never blocks
	pending := 0
	for i, dir := range dirs {
		if recentlyStalled(dir) {
			continue
		}
		pending++
		go func() { ch <- result{i, backupsIn(dir)} }()
	}
	deadline := time.After(3 * time.Second)
wait:
	for ; pending > 0; pending-- {
		select {
		case r := <-ch:
			results[r.i], done[r.i] = r.found, true
		case <-deadline:
			for i, dir := range dirs {
				if !done[i] && !recentlyStalled(dir) {
					markStalled(dir)
				}
			}
			break wait
		}
	}

	seen := map[string]bool{}
	var out []foundBackup
	for _, found := range results {
		for _, fb := range found {
			if fb.real != "" {
				if seen[fb.real] {
					continue
				}
				seen[fb.real] = true
			}
			out = append(out, fb)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Mod.After(out[j].Mod) })
	return out
}

// backupsIn lists the backups directly inside dir.
func backupsIn(dir string) []foundBackup {
	entries, _ := os.ReadDir(dir)
	var out []foundBackup
	for _, e := range entries {
		if !looksLikeBackupName(e.Name()) || strings.HasSuffix(e.Name(), ".partial") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		info, err := e.Info()
		if err != nil {
			continue
		}
		fb := foundBackup{Path: p, Mod: info.ModTime(), Size: info.Size()}
		if real, err := filepath.EvalSymlinks(p); err == nil {
			fb.real = real
		}
		switch backup.Detect(p) {
		case backup.FormatDir:
			if _, err := os.Stat(filepath.Join(p, "MANIFEST.txt")); err != nil {
				if entries, _ := os.ReadDir(p); len(entries) == 0 {
					continue
				}
			}
			fb.Kind = "folder"
		case backup.FormatTarGz:
			fb.Kind = "archive"
		case backup.FormatAge:
			fb.Kind = "encrypted"
		default:
			continue
		}
		out = append(out, fb)
	}
	return out
}

// shortHome writes a path under home as ~/…, which is how people read them.
func shortHome(env *sys.OS, p string) string {
	rel, err := filepath.Rel(env.Home(), p)
	switch {
	case err != nil || strings.HasPrefix(rel, ".."):
		return p
	case rel == ".":
		return "~"
	}
	return "~/" + rel
}

// pickBackup asks which backup to use. It lists what it found and always
// offers to type a path, because the one you want may be somewhere it did not
// look.
func pickBackup(env *sys.OS, title string) (string, error) {
	found := findBackups(env)
	var choices []tui.Choice
	for i, b := range found {
		if i == 12 {
			break
		}
		hint := fmt.Sprintf("%s · %s", b.Kind, b.Mod.Format("2 Jan 2006 15:04"))
		if b.Kind != "folder" {
			hint += " · " + humanBytes(b.Size)
		}
		choices = append(choices, tui.Choice{Label: shortenPath(shortHome(env, b.Path), 48), Value: b.Path, Hint: hint})
	}
	choices = append(choices, tui.Choice{Label: "Type a path…", Value: "\x00type", Hint: "a folder, .tar.gz or .age file"})
	desc := "Looked in ~/.local/share/dothaven, Downloads, Desktop, Documents and mounted drives."
	if len(found) == 0 {
		desc = "No backups found in the usual places (dothaven's folder, Downloads, Desktop, drives)."
	}
	choice, err := tui.Ask(title, desc, choices)
	if err != nil {
		return "", err
	}
	if choice != "\x00type" {
		return choice, nil
	}
	p, err := tui.Input("Path to the backup", "")
	if err != nil || p == "" {
		return "", err
	}
	if strings.HasPrefix(p, "~/") {
		p = filepath.Join(env.Home(), p[2:])
	}
	return filepath.Abs(strings.Trim(strings.TrimSpace(p), `"'`))
}

// openBackup makes any kind of backup readable as a directory. An archive is
// unpacked into a private temporary directory, removed by the returned cleanup
// (always safe to call). An encrypted one asks for the passphrase, up to three
// times on a terminal.
//
// The temporary directory is created 0700: an encrypted backup's contents are
// SSH keys and tokens, and /tmp is shared.
func openBackup(ctx context.Context, env *sys.OS, path string) (dir string, cleanup func(), err error) {
	return openBackupOnly(ctx, env, path)
}

// openBackupOnly is openBackup for a command that reads only some of a
// backup: an archive is still read end to end, but only the named folders
// (inventory, macos-defaults) are written out — decrypted credentials never
// touch the disk for a command that does not need them.
func openBackupOnly(ctx context.Context, env *sys.OS, path string, dirs ...string) (dir string, cleanup func(), err error) {
	if isGitHubSpec(path) {
		return openGitHubBackup(ctx, env, path, dirs...)
	}
	cleanup = func() {}
	switch backup.Detect(path) {
	case backup.FormatDir:
		return path, cleanup, nil
	case backup.FormatUnknown:
		if _, err := os.Stat(path); err != nil {
			return "", cleanup, fmt.Errorf("no backup at %s", path)
		}
		return "", cleanup, fmt.Errorf("%s is not a dothaven backup (expected a folder, .tar.gz or .age file)", path)
	}
	encrypted := backup.Detect(path) == backup.FormatAge
	attempts := 1
	if encrypted {
		if _, env := lookupSecretEnv(passphraseEnv); !env {
			attempts = 3
		}
		fmt.Fprintln(os.Stderr, dim("This backup is encrypted."))
	}
	var keep func(string) bool
	if len(dirs) > 0 {
		keep = backup.Only(dirs...)
	}
	for i := range attempts {
		tmp, done, err := sys.PrivateTempDir("restore")
		if err != nil {
			return "", cleanup, err
		}
		cleanup = done
		// Into a folder of its own: the temp folder also holds its marker,
		// and a loose file beside the backup would be taken for its root.
		root, err := backup.ExtractArchiveOnly(path, filepath.Join(tmp, "backup"), askPassphrase(), keep)
		if err == nil {
			return root, cleanup, nil
		}
		cleanup()
		cleanup = func() {}
		if errors.Is(err, backup.ErrWrongPassphrase) && i < attempts-1 {
			fmt.Fprintf(os.Stderr, "  %s wrong passphrase, try again.\n", warn("⚠"))
			continue
		}
		return "", cleanup, err
	}
	return "", cleanup, backup.ErrWrongPassphrase
}
