package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/doguyilmaz/dothaven/internal/backup"
	"github.com/doguyilmaz/dothaven/internal/github"
	"github.com/doguyilmaz/dothaven/internal/registry"
	"github.com/doguyilmaz/dothaven/internal/release"
	"github.com/doguyilmaz/dothaven/internal/restore"
	"github.com/doguyilmaz/dothaven/internal/sys"
	"github.com/doguyilmaz/dothaven/internal/tui"
	"github.com/spf13/cobra"
)

type checkStatus int

const (
	statusOK checkStatus = iota
	statusInfo
	statusWarn
	statusFail
)

// checkRow is one line of `dothaven doctor`: what was checked, what was found,
// and, when it is not fine, what to do about it.
type checkRow struct {
	Name   string
	Status checkStatus
	Detail string
	Fix    string
}

type checkSection struct {
	Title string
	Rows  []checkRow
}

func newDoctorCmd(env *sys.OS, version string) *cobra.Command {
	c := &cobra.Command{
		Use:   "doctor",
		Short: "Check dothaven itself: tools, folders, permissions, and what it can reach",
		Long: "Checks that dothaven can do its job on this machine, and says what to fix when\n" +
			"it cannot: the platform, its folders and their permissions, free disk space,\n" +
			"its own settings, the keychain it keeps tokens in, the tools each feature\n" +
			"relies on, the files it backs up (unreadable, too large), your backups, and\n" +
			"GitHub if you are signed in. Nothing is changed. Exits 1 if anything is broken.\n\n" +
			"To see what a backup had installed that this machine is missing, use\n" +
			"`dothaven missing <backup>` (this used to be `doctor <backup>`).",
		Args:          cobra.MaximumNArgs(1),
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				// The old meaning of doctor, kept working.
				fmt.Println(dim("(`doctor <backup>` is now `missing <backup>`)") + "\n")
				return runMissing(cmd, env, args[0])
			}
			sections := runSelfCheck(cmd.Context(), env, version)
			return printSelfCheck(sections)
		},
	}
	return c
}

// runSelfCheck gathers every section. Slow probes (tool versions, GitHub) run
// concurrently and each has its own deadline, so the whole check takes about
// a second and can never hang.
func runSelfCheck(ctx context.Context, env *sys.OS, version string) []checkSection {
	var (
		wg    sync.WaitGroup
		tools []checkRow
		gh    []checkRow
	)
	wg.Go(func() { tools = checkTools(ctx) })
	wg.Go(func() { gh = checkGitHub(ctx, env) })

	sections := []checkSection{
		{"dothaven", checkSelf(ctx, env, version)},
		{"This machine", checkMachine(env)},
		{"Folders", checkFolders(env)},
		{"Your settings", checkSettings(env)},
		{"What gets backed up", checkTargets(env)},
		{"Backups", checkBackups(env)},
	}
	wg.Wait()
	sections = append(sections, checkSection{"Tools", tools}, checkSection{"GitHub", gh})
	for _, s := range sections {
		for i := range s.Rows {
			s.Rows[i].Detail = tildeAll(env.Home(), s.Rows[i].Detail)
			s.Rows[i].Fix = tildeAll(env.Home(), s.Rows[i].Fix)
		}
	}
	return sections
}

// tildeAll writes every path under home in s as ~/…, so a line fits and reads
// the way people type it. The fixes stay valid shell: ~/ expands unquoted.
func tildeAll(home, s string) string {
	if len(home) < 2 {
		return s
	}
	home = strings.TrimSuffix(home, "/")
	if s == home {
		return "~"
	}
	s = strings.ReplaceAll(s, home+"/", "~/")
	if strings.HasSuffix(s, " "+home) {
		s = strings.TrimSuffix(s, home) + "~"
	}
	return s
}

func checkSelf(ctx context.Context, env *sys.OS, version string) []checkRow {
	method, exe := detectInstall(ctx, env)
	how := map[release.Method]string{release.Homebrew: "installed with Homebrew", release.GoInstall: "built with go install", release.Manual: "installed by hand"}[method]
	rows := []checkRow{{Name: "version", Status: statusOK, Detail: fmt.Sprintf("%s (%s)", version, how)}}
	if exe != "" {
		rows = append(rows, checkRow{Name: "binary", Status: statusOK, Detail: exe})
	}
	return rows
}

func checkMachine(env *sys.OS) []checkRow {
	var rows []checkRow
	switch runtime.GOOS {
	case "darwin", "linux":
		rows = append(rows, checkRow{Name: "platform", Status: statusOK, Detail: runtime.GOOS + " " + runtime.GOARCH})
	default:
		rows = append(rows, checkRow{Name: "platform", Status: statusFail, Detail: runtime.GOOS + " is not supported", Fix: "dothaven runs on macOS and Linux"})
	}
	home := env.Home()
	if fi, err := os.Stat(home); err != nil || !fi.IsDir() || home == "" {
		rows = append(rows, checkRow{Name: "home folder", Status: statusFail, Detail: "cannot find your home folder", Fix: "set HOME"})
	} else {
		rows = append(rows, checkRow{Name: "home folder", Status: statusOK, Detail: home})
	}
	if sh := os.Getenv("SHELL"); sh != "" {
		rows = append(rows, checkRow{Name: "shell", Status: statusInfo, Detail: sh})
	}
	term := "a terminal (menus and prompts available)"
	st := statusOK
	if !tui.Interactive() {
		term, st = "not a terminal (commands that change files need --yes)", statusInfo
	}
	rows = append(rows, checkRow{Name: "terminal", Status: st, Detail: term})
	for _, r := range checkBrewPaths(env) {
		rows = append(rows, checkRow{Name: "Homebrew path", Status: statusWarn,
			Detail: fmt.Sprintf("%s runs brew from %s; here it is in %s", r.File, r.Wrong, r.Right),
			Fix:    fmt.Sprintf("change %s/bin/brew to %s/bin/brew in %s", r.Wrong, r.Right, r.File)})
	}
	return rows
}

// checkDir reports whether dir exists, is writable, and is private.
func checkDir(name, dir string, mustBePrivate bool) []checkRow {
	fi, err := os.Stat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []checkRow{{Name: name, Status: statusInfo, Detail: dir + " (created when first needed)"}}
	}
	if err != nil || !fi.IsDir() {
		return []checkRow{{Name: name, Status: statusFail, Detail: dir + " is not a folder", Fix: "move it aside: mv " + dir + " " + dir + ".old"}}
	}
	f, err := os.CreateTemp(dir, ".dothaven-doctor-*")
	if err != nil {
		return []checkRow{{Name: name, Status: statusFail, Detail: dir + " is not writable", Fix: "chown -R $(whoami) " + dir}}
	}
	f.Close()
	os.Remove(f.Name())
	row := checkRow{Name: name, Status: statusOK, Detail: dir}
	if mustBePrivate && fi.Mode().Perm()&0o077 != 0 {
		row.Status = statusWarn
		row.Detail = fmt.Sprintf("%s is readable by other users (%04o) and holds backups", dir, fi.Mode().Perm())
		row.Fix = "chmod 700 " + dir
	}
	return []checkRow{row}
}

func freeBytes(dir string) (uint64, bool) {
	for d := dir; ; d = filepath.Dir(d) {
		var st syscall.Statfs_t
		if err := syscall.Statfs(d, &st); err == nil {
			return st.Bavail * uint64(st.Bsize), true
		}
		if d == filepath.Dir(d) {
			return 0, false
		}
	}
}

func checkFolders(env *sys.OS) []checkRow {
	var rows []checkRow
	rows = append(rows, checkDir("data", env.DataDir(), true)...)
	rows = append(rows, checkDir("settings", configDir(env), false)...)
	rows = append(rows, checkDir("temporary", os.TempDir(), false)...)
	if free, ok := freeBytes(env.DataDir()); ok {
		row := checkRow{Name: "free space", Status: statusOK, Detail: humanBytes(int64(free)) + " available"}
		switch {
		case free < 100<<20:
			row.Status, row.Fix = statusFail, "free some disk space before backing up or restoring"
		case free < 1<<30:
			row.Status, row.Fix = statusWarn, "under 1 GB: a backup with large config folders may not fit"
		}
		rows = append(rows, row)
	}
	return rows
}

func checkSettings(env *sys.OS) []checkRow {
	var rows []checkRow
	// Include list: count the lines the parser had to drop.
	if b, err := os.ReadFile(includePath(env)); err == nil {
		inc := registry.ParseIncludes(string(b), env.Home())
		lines := 0
		for _, l := range strings.Split(string(b), "\n") {
			if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
				lines++
			}
		}
		row := checkRow{Name: "include list", Status: statusOK, Detail: fmt.Sprintf("%s, %d left out on purpose", plural(len(inc.Paths), "path"), len(inc.Declined))}
		if dropped := lines - len(inc.Paths) - len(inc.Declined); dropped > 0 {
			row.Status = statusWarn
			row.Detail += fmt.Sprintf("; %s ignored (outside your home folder, or duplicated)", plural(dropped, "line"))
			row.Fix = "edit " + includePath(env)
		}
		rows = append(rows, row)
	} else {
		rows = append(rows, checkRow{Name: "include list", Status: statusInfo, Detail: "none yet (`dothaven include --list` shows what isn't covered)"})
	}
	validJSON := func(name, path string) {
		b, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		var v any
		if err != nil || json.Unmarshal(b, &v) != nil {
			rows = append(rows, checkRow{Name: name, Status: statusWarn, Detail: path + " is damaged", Fix: "delete it; dothaven will start it afresh"})
			return
		}
		rows = append(rows, checkRow{Name: name, Status: statusOK, Detail: path})
	}
	validJSON("GitHub settings", githubConfigPath(env))
	validJSON("restore ledger", ledgerPath(env))
	if lg := restore.LoadLedger(ledgerPath(env)); len(lg.Applied) > 0 {
		rows = append(rows, checkRow{Name: "applied here", Status: statusInfo, Detail: plural(len(lg.Applied), "file") + " restored on this machine"})
	}
	st := secrets(env)
	row := checkRow{Name: "keychain", Status: statusOK, Detail: "tokens and passphrases go to " + st.Name()}
	if st.IsFile() {
		row.Detail = "tokens and passphrases go to owner-only files in " + shortHome(env, st.Dir())
		row.Status = statusWarn
		row.Fix = "install a keyring (gnome-keyring / KWallet with secret-tool) for safer storage"
	}
	rows = append(rows, row)
	return rows
}

func checkTargets(env *sys.OS) []checkRow {
	var present, files, credentials, rebuilt int
	var unreadable, large []string
	for _, t := range registry.BackupTargets(env.Home(), allEntries(env)) {
		walked, skipped := backup.Walk(t, backup.WalkOptions{})
		if len(walked) > 0 {
			present++
		}
		files += len(walked)
		if t.Sensitivity == registry.High {
			credentials += len(walked)
		}
		for _, s := range skipped {
			switch {
			case s.Reason == "too large":
				large = append(large, fmt.Sprintf("%s (%s)", s.Dest, humanBytes(s.Size)))
			case backup.IsRebuildable(s):
				rebuilt++
			default:
				unreadable = append(unreadable, s.Dest)
			}
		}
		for _, f := range walked {
			if fh, err := os.Open(f.Path); err != nil {
				unreadable = append(unreadable, f.Dest)
			} else {
				fh.Close()
			}
		}
	}
	rows := []checkRow{{Name: "tracked", Status: statusOK, Detail: fmt.Sprintf("%s from %d sources on this machine (%d with credentials)", plural(files, "file"), present, credentials)}}
	if rebuilt > 0 {
		rows = append(rows, checkRow{Name: "rebuildable", Status: statusInfo, Detail: fmt.Sprintf("%s left out (node_modules, caches, build output); reinstalled on the new machine", plural(rebuilt, "folder"))})
	}
	if len(unreadable) > 0 {
		rows = append(rows, checkRow{Name: "unreadable", Status: statusWarn, Detail: fmt.Sprintf("%s: %s", plural(len(unreadable), "file"), preview(unreadable, 3)),
			Fix: "these would be left out of a backup; check their owner and permissions"})
	}
	if len(large) > 0 {
		rows = append(rows, checkRow{Name: "too large", Status: statusWarn, Detail: fmt.Sprintf("%s over %s: %s", plural(len(large), "file"), humanBytes(backup.MaxFileSize), preview(large, 3)),
			Fix: "left out of backups; exclude or move them if they are not config"})
	}
	if u := uncovered(env); len(u) > 0 {
		rows = append(rows, checkRow{Name: "not covered", Status: statusInfo, Detail: fmt.Sprintf("%s %s like config but %s in no backup", plural(len(u), "path"), pick(len(u), "looks", "look"), pick(len(u), "is", "are")), Fix: "dothaven include --list"})
	}
	return rows
}

func preview(items []string, n int) string {
	if len(items) <= n {
		return strings.Join(items, ", ")
	}
	return strings.Join(items[:n], ", ") + fmt.Sprintf(" +%d more", len(items)-n)
}

func checkBackups(env *sys.OS) []checkRow {
	found := findBackups(env)
	if len(found) == 0 {
		return []checkRow{{Name: "newest", Status: statusWarn, Detail: "no backup found on this machine or its drives", Fix: "dothaven backup --encrypt"}}
	}
	b := found[0]
	row := checkRow{Name: "newest", Status: statusOK, Detail: fmt.Sprintf("%s, %s old (%s)", b.Kind, humanAge(time.Since(b.Mod)), shortHome(env, b.Path))}
	if time.Since(b.Mod) > 7*24*time.Hour {
		row.Status, row.Fix = statusWarn, "dothaven backup --encrypt"
	}
	rows := []checkRow{row}
	if b.Kind == "folder" {
		if _, err := os.Stat(filepath.Join(b.Path, "MANIFEST.txt")); err != nil {
			rows = append(rows, checkRow{Name: "manifest", Status: statusWarn, Detail: "the newest folder has no MANIFEST.txt (made by an older dothaven, or incomplete)"})
		}
	}
	return rows
}

// toolSpec is an external tool and what depends on it.
type toolSpec struct {
	name, args, enables string
	need                checkStatus // status when missing
	os                  string      // "" = any
}

var toolSpecs = []toolSpec{
	{"git", "--version", "`ready` (unpushed work)", statusWarn, ""},
	{"brew", "--version", "app & package inventory, `reinstall`", statusWarn, "darwin"},
	{"defaults", "", "macOS settings in backups", statusWarn, "darwin"},
	{"zsh", "--version", "`check` of zsh files", statusInfo, ""},
	{"ssh", "-V", "`check` of ssh config", statusInfo, ""},
	{"gh", "--version", "`github login --gh` (optional)", statusInfo, ""},
	{"chezmoi", "--version", "the optional chezmoi sync", statusInfo, ""},
}

func checkTools(ctx context.Context) []checkRow {
	rows := make([]checkRow, 0, len(toolSpecs)+1)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, t := range toolSpecs {
		if t.os != "" && t.os != runtime.GOOS {
			continue
		}
		wg.Go(func() {
			row := checkRow{Name: t.name}
			path, err := exec.LookPath(t.name)
			switch {
			case err != nil:
				row.Status, row.Detail = t.need, "not found; needed for "+t.enables
				if t.name == "brew" {
					row.Fix = homebrewInstall
				}
			default:
				row.Status, row.Detail = statusOK, path
				if t.args != "" {
					vctx, cancel := context.WithTimeout(ctx, 5*time.Second)
					cmd := exec.CommandContext(vctx, t.name, t.args)
					cmd.WaitDelay = time.Second
					out, _ := cmd.CombinedOutput()
					cancel()
					if v := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0]); v != "" {
						row.Detail = v
					}
				}
			}
			mu.Lock()
			rows = append(rows, row)
			mu.Unlock()
		})
	}
	wg.Wait()
	// Stable order: as declared.
	order := map[string]int{}
	for i, t := range toolSpecs {
		order[t.name] = i
	}
	sortRows(rows, order)
	rows = append(rows, checkRow{Name: "age", Status: statusOK, Detail: "built in, so encrypted backups need no extra install"})
	return rows
}

func sortRows(rows []checkRow, order map[string]int) {
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && order[rows[j].Name] < order[rows[j-1].Name]; j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
}

func checkGitHub(ctx context.Context, env *sys.OS) []checkRow {
	tok, src, err := resolveToken(ctx, env, false)
	if errors.Is(err, errRenewDue) {
		return []checkRow{{Name: "sign-in", Status: statusWarn, Detail: "GitHub App sign-ins last 8 hours; this one is due for renewal", Fix: "dothaven github status"}}
	}
	if tok == "" {
		return []checkRow{{Name: "sign-in", Status: statusInfo, Detail: "not signed in (optional)", Fix: "dothaven github login"}}
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	c, me, err := githubClientNoPrompt(ctx, tok)
	if err != nil {
		var apiErr *github.APIError
		if errors.As(err, &apiErr) && apiErr.Status == 401 {
			return []checkRow{{Name: "sign-in", Status: statusFail, Detail: "the token from " + src + " was rejected (expired or revoked)", Fix: "dothaven github login"}}
		}
		return []checkRow{{Name: "sign-in", Status: statusWarn, Detail: "could not reach GitHub: " + err.Error()}}
	}
	rows := []checkRow{{Name: "sign-in", Status: statusOK, Detail: me.Login + " (via " + src + ")"}}
	repo := firstNonEmpty(loadGitHubConfig(env).Repo, me.Login+"/"+defaultRepoName)
	r, err := c.GetRepo(ctx, repo)
	switch {
	case errors.Is(err, github.ErrNotFound):
		rows = append(rows, checkRow{Name: "repository", Status: statusInfo, Detail: repo + " not created yet", Fix: "dothaven github push"})
	case err != nil:
		rows = append(rows, checkRow{Name: "repository", Status: statusWarn, Detail: err.Error()})
	case !r.Private:
		rows = append(rows, checkRow{Name: "repository", Status: statusFail, Detail: repo + " is PUBLIC, so dothaven will not write to it", Fix: "make it private on GitHub"})
	default:
		rows = append(rows, checkRow{Name: "repository", Status: statusOK, Detail: repo + " (private)"})
	}
	return rows
}

func printSelfCheck(sections []checkSection) error {
	fmt.Println(bold("dothaven doctor") + dim(": can dothaven do its job here?"))
	var fails, warns int
	width := 0
	for _, s := range sections {
		for _, r := range s.Rows {
			width = max(width, len([]rune(r.Name)))
		}
	}
	for _, s := range sections {
		if len(s.Rows) == 0 {
			continue
		}
		fmt.Printf("\n%s\n", bold(s.Title))
		for _, r := range s.Rows {
			var mark string
			switch r.Status {
			case statusOK:
				mark = good("✓")
			case statusInfo:
				mark = dim("·")
			case statusWarn:
				mark = warn("⚠")
				warns++
			case statusFail:
				mark = danger("✗")
				fails++
			}
			plain := func(s string) string { return s }
			fmt.Printf("  %s %-*s  %s\n", mark, width, r.Name, paragraph(r.Detail, strings.Repeat(" ", width+6), plain))
			if r.Fix != "" && r.Status >= statusWarn {
				fmt.Printf("      %s %s\n", dim("fix:"), paragraph(r.Fix, "           ", kbd))
			} else if r.Fix != "" {
				fmt.Printf("      %s\n", paragraph(r.Fix, "      ", dim))
			}
		}
	}
	fmt.Println()
	switch {
	case fails > 0:
		fmt.Println(danger(fmt.Sprintf("✗ %s, %s. Fixes are listed above.", plural(fails, "problem"), plural(warns, "warning"))))
		return ExitError{Code: 1}
	case warns > 0:
		fmt.Println(warn(fmt.Sprintf("⚠ Works, with %s worth a look above.", plural(warns, "warning"))))
	default:
		fmt.Println(good("✓ Everything dothaven needs is in place."))
	}
	return nil
}
