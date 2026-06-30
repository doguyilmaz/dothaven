package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// latestBackup returns the most recently modified backup-* directory in dir, or
// "" if none. Sorting by mtime (not name) is correct when a dir holds backups
// from several machines: a name sort orders by host before timestamp, so it
// would pick the alphabetically-last host's backup rather than the newest.
// Archives (.tar.gz) are ignored — they can't be diffed/restored without extraction.
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
