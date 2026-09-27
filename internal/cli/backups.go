package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
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
}

// readDirTimeout is os.ReadDir that gives up after d. A disconnected network
// share under /Volumes can block a directory read for minutes; looking for
// backups must never be the thing that hangs.
func readDirTimeout(dir string, d time.Duration) []os.DirEntry {
	ch := make(chan []os.DirEntry, 1) // buffered: a late reader never blocks
	go func() {
		e, _ := os.ReadDir(dir)
		ch <- e
	}()
	select {
	case e := <-ch:
		return e
	case <-time.After(d):
		return nil
	}
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

// findBackups lists every backup it can find, newest first.
func findBackups(env *sys.OS) []foundBackup {
	seen := map[string]bool{}
	var out []foundBackup
	for _, dir := range backupSearchDirs(env) {
		for _, e := range readDirTimeout(dir, 2*time.Second) {
			if !looksLikeBackupName(e.Name()) || strings.HasSuffix(e.Name(), ".partial") {
				continue
			}
			p := filepath.Join(dir, e.Name())
			if real, err := filepath.EvalSymlinks(p); err == nil {
				if seen[real] {
					continue
				}
				seen[real] = true
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			fb := foundBackup{Path: p, Mod: info.ModTime(), Size: info.Size()}
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
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Mod.After(out[j].Mod) })
	return out
}

// shortHome writes a path under home as ~/…, which is how people read them.
func shortHome(env *sys.OS, p string) string {
	if rel, err := filepath.Rel(env.Home(), p); err == nil && !strings.HasPrefix(rel, "..") {
		return "~/" + rel
	}
	return p
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
func openBackup(path string) (dir string, cleanup func(), err error) {
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
		if _, env := os.LookupEnv(passphraseEnv); !env {
			attempts = 3
		}
		fmt.Fprintln(os.Stderr, dim("This backup is encrypted."))
	}
	for i := range attempts {
		tmp, err := os.MkdirTemp("", "dothaven-restore-")
		if err != nil {
			return "", cleanup, err
		}
		cleanup = func() { _ = os.RemoveAll(tmp) }
		if err := os.Chmod(tmp, 0o700); err != nil {
			cleanup()
			return "", func() {}, err
		}
		root, err := backup.ExtractArchive(path, tmp, askPassphrase())
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
