// Package gitwork finds work that exists only on this machine.
//
// Everything else dothaven handles is recoverable: a config you forget can be
// written again, a package you miss can be reinstalled. Uncommitted changes,
// unpushed commits and stashes cannot. They are the only thing a wipe destroys
// permanently, and they are what a migration checklist is for.
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
	"time"
)

// Repo is one repository's unsaved work.
type Repo struct {
	Path      string
	Dirty     int  // modified, staged or untracked files
	Unsaved   int  // commits on a local branch that are on no remote
	Stashes   int  // stash entries
	HasRemote bool // false means the whole repository exists only here
	// Ignored lists files git deliberately does not track that hold what a
	// clone cannot give back: .env files, keys, local terraform state. They
	// are ignored because they are secret or machine-local, which is
	// also why a wipe followed by `git clone` loses them.
	Ignored []string
	Err     string
}

// AtRisk reports whether this repo holds anything a wipe would destroy.
func (r Repo) AtRisk() bool {
	return r.Dirty > 0 || r.Unsaved > 0 || r.Stashes > 0 || !r.HasRemote || len(r.Ignored) > 0
}

// IsLocalOnlyName reports whether an ignored file's name marks it as the kind
// that exists only on one machine and matters: environment files, private keys
// and keystores, cloud credentials, local terraform state.
func IsLocalOnlyName(name string) bool {
	n := strings.ToLower(name)
	for _, sample := range []string{".example", ".sample", ".template", ".dist", ".defaults", ".tpl"} {
		if strings.HasSuffix(n, sample) {
			return false
		}
	}
	switch n {
	case ".env", ".envrc", ".dev.vars", ".npmrc", ".pypirc", ".netrc", "master.key",
		"google-services.json", "googleservice-info.plist", "id_rsa", "id_ed25519", ".secrets":
		return true
	}
	if strings.HasPrefix(n, ".env.") || strings.HasPrefix(n, "service-account") || strings.HasPrefix(n, "secrets.") {
		return true
	}
	for _, ext := range []string{".pem", ".key", ".p12", ".pfx", ".p8", ".jks", ".keystore",
		".mobileprovision", ".tfstate", ".tfstate.backup", ".tfvars", ".secret"} {
		if strings.HasSuffix(n, ext) {
			return true
		}
	}
	return strings.Contains(n, "credentials") && strings.HasSuffix(n, ".json")
}

// Runner executes git in a directory. Injected so the walk and the reporting
// can be tested without building repositories on disk.
type Runner func(ctx context.Context, dir string, args ...string) (string, error)

// gitTimeout bounds one git call. A repo on a stalled network mount, or one so
// large that `git status` crawls, must cost one repo's answer, not the run.
const gitTimeout = 20 * time.Second

// GitRunner runs the real git.
func GitRunner(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	// A repo whose remote needs a password must not hang the whole scan.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	// fsmonitor daemons inherit stdout; without WaitDelay a daemon git forks
	// keeps Wait blocked after the timeout kills git itself.
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// skipDirs are subtrees that never contain a repo worth reporting: dependency
// trees vendor their own, caches and toolchains are regenerated, and media
// libraries are enormous and hold no code. Skipping them is most of what keeps
// a walk of the whole home directory to a second or two.
var skipDirs = map[string]bool{
	"node_modules": true, "vendor": true, ".cache": true, "Caches": true,
	"Library": true, ".Trash": true, ".venv": true, "venv": true,
	"__pycache__": true, ".terraform": true, "Pods": true, ".gradle": true,
	"CloudStorage": true, ".npm": true, ".bun": true, ".cargo": true,
	".rustup": true, ".nvm": true, ".pyenv": true, ".rbenv": true, ".gem": true,
	".m2": true, ".pub-cache": true, ".cocoapods": true, ".android": true,
	".docker": true, ".vscode": true, ".cursor": true, ".deno": true,
	".oh-my-zsh": true, ".zinit": true, ".antidote": true, ".zplug": true,
	"Applications": true, "Pictures": true, "Movies": true, "Music": true,
	"DerivedData": true, "build": true, "dist": true, "target": true, ".next": true,
}

// skipPaths are subtrees skipped by their path below a root: plugin managers
// whose every plugin is a clean clone.
var skipPaths = []string{".local/share/nvim", ".vim/plugged", ".tmux/plugins", "go/pkg", ".local/share/Trash"}

// Find walks roots for git repositories. A repository is found when it sits
// at most maxDepth levels below a root (~/code/org/repo is depth 3). Depth is
// bounded because a home directory contains tens of thousands of directories
// and a migration check that takes minutes is one nobody runs.
//
// A directory is recognised by its .git entry before the depth limit is
// applied (checking afterwards missed every repository sitting exactly at the
// limit), and a .git file (a worktree or submodule) counts as well.
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
			if d.Name() == ".git" {
				return fs.SkipDir
			}
			// Recognised before any pruning: a repository that happens to be
			// called "build" is still a repository.
			if _, gerr := os.Lstat(filepath.Join(path, ".git")); gerr == nil && !seen[path] {
				seen[path] = true
				found = append(found, path)
			}
			if path != root && skipDirs[d.Name()] {
				return fs.SkipDir
			}
			if rel, rerr := filepath.Rel(root, path); rerr == nil {
				for _, sp := range skipPaths {
					if filepath.ToSlash(rel) == sp {
						return fs.SkipDir
					}
				}
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
// state is read. Nothing is fetched, so this is fast and works offline. That
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

	// --directory reports an ignored directory once instead of listing it,
	// so a node_modules costs one line rather than a walk of its contents.
	if s, err := run(ctx, path, "ls-files", "--others", "--ignored", "--exclude-standard", "--directory", "--no-empty-directory"); err == nil {
		for _, l := range strings.Split(s, "\n") {
			l = strings.TrimSpace(l)
			if l == "" || strings.HasSuffix(l, "/") {
				continue
			}
			if IsLocalOnlyName(filepath.Base(l)) {
				r.Ignored = append(r.Ignored, l)
			}
		}
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
