// Package gitwork finds work that exists only on this machine.
//
// Everything else dothaven handles is recoverable: a config you forget can be
// written again, a package you miss can be reinstalled. Uncommitted changes,
// unpushed commits and stashes cannot. They are the only thing a wipe destroys
// permanently, and they are what a migration checklist is really for.
package gitwork

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// Repo is one repository's unsaved work.
type Repo struct {
	Path      string
	Dirty     int  // modified, staged or untracked files
	Unsaved   int  // commits on a local branch that are on no remote
	Stashes   int  // stash entries
	HasRemote bool // false means the whole repository exists only here
	Err       string
}

// AtRisk reports whether this repo holds anything a wipe would destroy.
func (r Repo) AtRisk() bool {
	return r.Dirty > 0 || r.Unsaved > 0 || r.Stashes > 0 || !r.HasRemote
}

// Runner executes git in a directory. Injected so the walk and the reporting
// can be tested without building repositories on disk.
type Runner func(ctx context.Context, dir string, args ...string) (string, error)

// GitRunner runs the real git.
func GitRunner(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	// A repo whose remote needs a password must not hang the whole scan.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// skipDirs are subtrees that never contain a repo worth reporting: dependency
// trees vendor their own, and caches are regenerated.
var skipDirs = map[string]bool{
	"node_modules": true, "vendor": true, ".cache": true, "Caches": true,
	"Library": true, ".Trash": true, ".venv": true, "venv": true,
	"__pycache__": true, ".terraform": true, "Pods": true, ".gradle": true,
	"CloudStorage": true, ".npm": true, ".bun": true, ".cargo": true,
}

// Find walks roots for git repositories, no deeper than maxDepth below each
// root. Depth is bounded because a home directory contains tens of thousands of
// directories and a migration check that takes minutes is one nobody runs.
func Find(ctx context.Context, roots []string, maxDepth int) []string {
	seen := map[string]bool{}
	var found []string
	for _, root := range roots {
		root = filepath.Clean(root)
		rootDepth := strings.Count(root, string(os.PathSeparator))
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil || !d.IsDir() {
				return nil
			}
			if skipDirs[d.Name()] {
				return fs.SkipDir
			}
			if d.Name() == ".git" {
				repo := filepath.Dir(path)
				if !seen[repo] {
					seen[repo] = true
					found = append(found, repo)
				}
				return fs.SkipDir // never descend into a repo's own .git
			}
			if strings.Count(path, string(os.PathSeparator))-rootDepth >= maxDepth {
				return fs.SkipDir
			}
			return nil
		})
	}
	return found
}

// Inspect reports the unsaved work in each repo, concurrently. Only local git
// state is read — nothing is fetched, so this is fast and works offline. That
// means "unpushed" is measured against the last known remote state.
func Inspect(ctx context.Context, run Runner, paths []string, progress *int64) []Repo {
	out := make([]Repo, len(paths))
	sem := make(chan struct{}, runtime.NumCPU())
	var wg sync.WaitGroup
	for i, p := range paths {
		wg.Add(1)
		go func(i int, p string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out[i] = inspectOne(ctx, run, p)
			if progress != nil {
				atomic.AddInt64(progress, 1)
			}
		}(i, p)
	}
	wg.Wait()

	var risky []Repo
	for _, r := range out {
		if r.AtRisk() || r.Err != "" {
			risky = append(risky, r)
		}
	}
	return risky
}

func inspectOne(ctx context.Context, run Runner, path string) Repo {
	r := Repo{Path: path}

	if s, err := run(ctx, path, "status", "--porcelain"); err == nil {
		r.Dirty = countLines(s)
	} else {
		r.Err = "git status failed"
		return r
	}

	// No remote means the entire repository exists only on this machine, and
	// "push it" is not even advice that would work yet. It is a different and
	// worse situation than some unpushed commits, so it is tracked separately.
	if s, err := run(ctx, path, "remote"); err == nil {
		r.HasRemote = strings.TrimSpace(s) != ""
	}

	// Commits on any local branch that are on no remote ref. This is the
	// question that matters, and it is not the same as "ahead of upstream":
	// a branch with no upstream configured may be fully pushed, while one
	// that tracks a remote can still hold work nowhere else. Asking about
	// upstreams reported both wrongly.
	if s, err := run(ctx, path, "rev-list", "--count", "--branches", "--not", "--remotes"); err == nil {
		r.Unsaved, _ = strconv.Atoi(strings.TrimSpace(s))
	}

	if s, err := run(ctx, path, "stash", "list"); err == nil {
		r.Stashes = countLines(s)
	}
	return r
}

func countLines(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	return len(strings.Split(s, "\n"))
}
