package backup

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/doguyilmaz/dothaven/internal/registry"
)

// MaxFileSize caps a single file copied into a backup. Config files are
// kilobytes; the cap exists so a stray disk image or model file inside a config
// directory cannot balloon a backup. Anything over it is reported, never
// dropped silently.
const MaxFileSize = 64 << 20 // 64 MiB

// File is one regular file a target resolves to.
type File struct {
	Path string // absolute path on this machine (the logical path, not the link target)
	Dest string // slash-separated path inside the backup
	Exec bool   // any executable bit set — hooks and scripts must stay runnable
	Size int64
}

// Skipped is a file that exists but will not be carried, with the reason.
type Skipped struct {
	Dest   string
	Reason string
	Size   int64
}

// WalkOptions tune which files a target yields.
type WalkOptions struct {
	MaxSize int64 // 0 → MaxFileSize
	// SkipVCS drops .git/.hg/.svn directories. A backup keeps them (a plugin
	// marketplace clone must still update after restore); a chezmoi source,
	// itself a git repo, must not nest them.
	SkipVCS bool
}

var vcsDirs = map[string]bool{".git": true, ".hg": true, ".svn": true}

// alwaysSkip are entries no config backup wants: Finder litter and editor swap.
var alwaysSkip = map[string]bool{".DS_Store": true}

// Walk resolves a target into the files it would carry.
//
// Symlinks are followed, both at the root and inside it. dotfiles managed by
// stow or a bare repo are symlinks — ~/.config/nvim pointing into ~/dotfiles —
// and a walker that does not follow them backs up nothing at all for exactly
// the people with the most carefully kept config. Directory links are followed
// once per real directory, so a link cycle cannot loop.
//
// Sockets, FIFOs and devices are skipped without comment: they are runtime
// endpoints (gpg-agent, ssh control masters), not data, and reading one blocks.
func Walk(t registry.BackupTarget, opts WalkOptions) ([]File, []Skipped) {
	if opts.MaxSize <= 0 {
		opts.MaxSize = MaxFileSize
	}
	info, err := os.Stat(t.Src)
	if err != nil {
		return nil, nil // not installed
	}
	w := &walker{opts: opts, exclude: t.Exclude, seen: map[string]bool{}}
	if !t.IsDir {
		if info.Mode().IsRegular() {
			w.file(t.Src, t.Dest, info)
		}
		return w.files, w.skipped
	}
	if !info.IsDir() {
		return nil, nil
	}
	w.dir(t.Src, t.Dest, "")
	sort.Slice(w.files, func(i, j int) bool { return w.files[i].Dest < w.files[j].Dest })
	return w.files, w.skipped
}

type walker struct {
	opts    WalkOptions
	exclude []string
	seen    map[string]bool
	files   []File
	skipped []Skipped
}

func (w *walker) file(p, dest string, info fs.FileInfo) {
	if info.Size() > w.opts.MaxSize {
		w.skipped = append(w.skipped, Skipped{Dest: dest, Reason: "too large", Size: info.Size()})
		return
	}
	w.files = append(w.files, File{Path: p, Dest: dest, Exec: info.Mode().Perm()&0o111 != 0, Size: info.Size()})
}

// dir walks one directory. rel is the slash path below the target root, which
// is what exclude patterns match against.
func (w *walker) dir(p, dest, rel string) {
	real, err := filepath.EvalSymlinks(p)
	if err != nil || w.seen[real] {
		return
	}
	w.seen[real] = true
	entries, err := os.ReadDir(p)
	if err != nil {
		w.skipped = append(w.skipped, Skipped{Dest: dest, Reason: "unreadable"})
		return
	}
	for _, e := range entries {
		name := e.Name()
		childRel := name
		if rel != "" {
			childRel = rel + "/" + name
		}
		if alwaysSkip[name] || Excluded(childRel, w.exclude) {
			continue
		}
		childPath := filepath.Join(p, name)
		childDest := dest + "/" + name
		info, err := os.Stat(childPath) // follows links
		if err != nil {
			continue // broken link
		}
		switch {
		case info.IsDir():
			if w.opts.SkipVCS && vcsDirs[name] {
				continue
			}
			w.dir(childPath, childDest, childRel)
		case info.Mode().IsRegular():
			w.file(childPath, childDest, info)
		}
	}
}

// Excluded reports whether rel (slash-separated, relative to a target root)
// matches one of the patterns. A pattern with no slash matches any single path
// segment ("logs" drops every logs/ directory, "*.log" every log file); one with
// a slash matches from the root ("bin/flyctl", or "cache" as a prefix).
func Excluded(rel string, patterns []string) bool {
	for _, pat := range patterns {
		if strings.Contains(pat, "/") {
			if ok, _ := path.Match(pat, rel); ok || strings.HasPrefix(rel, strings.TrimSuffix(pat, "/")+"/") {
				return true
			}
			continue
		}
		for _, seg := range strings.Split(rel, "/") {
			if ok, _ := path.Match(pat, seg); ok {
				return true
			}
		}
	}
	return false
}
