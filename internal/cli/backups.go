package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// latestBackup returns the newest backup-* directory in dir (timestamped names
// sort chronologically), or "" if none. Archives (.tar.gz) are ignored — they
// can't be diffed/restored without extraction.
func latestBackup(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "backup-") {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	return filepath.Join(dir, names[0])
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
