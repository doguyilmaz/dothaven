package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/doguyilmaz/dothaven/internal/backup"
	"github.com/doguyilmaz/dothaven/internal/dashboard"
	"github.com/doguyilmaz/dothaven/internal/gitwork"
	"github.com/doguyilmaz/dothaven/internal/registry"
	"github.com/doguyilmaz/dothaven/internal/restore"
	"github.com/doguyilmaz/dothaven/internal/scan"
	"github.com/doguyilmaz/dothaven/internal/snapshot"
	"github.com/doguyilmaz/dothaven/internal/sys"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newUICmd(env *sys.OS, version string) *cobra.Command {
	var noOpen bool
	c := &cobra.Command{
		Use:     "ui",
		Aliases: []string{"dashboard"},
		Short:   "Open a live dashboard in your browser (local and read-only)",
		Long: "Serves a dashboard to your browser from this machine only: what your backups\n" +
			"cover and what they miss, the backups it can find, secrets sitting in plain\n" +
			"files, repositories with unpushed work, installed software, what restore has\n" +
			"applied, and your GitHub backup repo.\n\n" +
			"It listens on 127.0.0.1 with a one-time key in the link, never writes\n" +
			"anything, and loads nothing from the internet. Stop it with Enter or Ctrl-C.",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()
			srv, err := dashboard.Start(ctx, dashboardSources(env, version))
			if err != nil {
				return err
			}
			fmt.Printf("%s Dashboard running at\n  %s\n", good("✓"), kbd(srv.URL))
			if !noOpen {
				openBrowser(srv.URL)
			}
			if term.IsTerminal(int(os.Stdin.Fd())) {
				fmt.Println(dim("  Press Enter (or Ctrl-C) to stop."))
				go func() {
					_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
					cancel()
				}()
			} else {
				fmt.Println(dim("  Ctrl-C to stop."))
			}
			<-ctx.Done()
			fmt.Println("Dashboard stopped.")
			return nil
		},
	}
	c.Flags().BoolVar(&noOpen, "no-open", false, "print the link but don't open a browser")
	return c
}

func dashboardSources(env *sys.OS, version string) map[string]dashboard.Source {
	return map[string]dashboard.Source{
		"summary": func(ctx context.Context) (any, error) { return dashSummary(ctx, env, version), nil },
		"secrets": func(ctx context.Context) (any, error) { return dashSecrets(ctx, env) },
		"repos":   func(ctx context.Context) (any, error) { return dashRepos(ctx, env), nil },
		"github":  func(ctx context.Context) (any, error) { return dashGitHub(ctx, env), nil },
	}
}

type dashCategory struct {
	Name        string `json:"name"`
	Files       int    `json:"files"`
	Bytes       int64  `json:"bytes"`
	About       string `json:"about"`
	Credentials bool   `json:"credentials"`
}

type dashBackup struct {
	Path     string    `json:"path"`
	Kind     string    `json:"kind"`
	Modified time.Time `json:"modified"`
	Size     int64     `json:"size"`
}

type dashGroup struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

type dashSummaryData struct {
	Machine struct {
		Host    string `json:"host"`
		OS      string `json:"os"`
		Version string `json:"version"`
	} `json:"machine"`
	Coverage struct {
		Files       int            `json:"files"`
		Bytes       int64          `json:"bytes"`
		Credentials int            `json:"credentials"`
		Includes    int            `json:"includes"`
		Categories  []dashCategory `json:"categories"`
		Uncovered   []string       `json:"uncovered"`
	} `json:"coverage"`
	Backups []dashBackup `json:"backups"`
	Ledger  struct {
		Applied     int        `json:"applied"`
		Skipped     int        `json:"skipped"`
		LastApplied *time.Time `json:"lastApplied,omitempty"`
	} `json:"ledger"`
	Inventory struct {
		Source string      `json:"source"`
		Date   string      `json:"date"`
		Groups []dashGroup `json:"groups"`
	} `json:"inventory"`
}

// dashSummary is the fast panel: it walks the tracked paths without reading
// file contents, lists backups, and reads the ledger and the newest inventory.
func dashSummary(ctx context.Context, env *sys.OS, version string) dashSummaryData {
	var d dashSummaryData
	d.Machine.Host, d.Machine.OS, d.Machine.Version = hostname(), runtime.GOOS+" "+runtime.GOARCH, version

	per := map[string]*dashCategory{}
	seen := map[string]bool{}
	for _, t := range registry.BackupTargets(env.Home(), allEntries(env)) {
		if ctx.Err() != nil {
			break
		}
		files, _ := backup.Walk(t, backup.WalkOptions{})
		for _, f := range files {
			if seen[f.Path] {
				continue
			}
			seen[f.Path] = true
			c := per[t.Category]
			if c == nil {
				c = &dashCategory{Name: t.Category, About: categoryAbout[t.Category]}
				per[t.Category] = c
			}
			c.Files++
			c.Bytes += f.Size
			if t.Sensitivity == registry.High {
				c.Credentials = true
				d.Coverage.Credentials++
			}
			d.Coverage.Files++
			d.Coverage.Bytes += f.Size
		}
	}
	d.Coverage.Categories = []dashCategory{}
	for _, c := range per {
		d.Coverage.Categories = append(d.Coverage.Categories, *c)
	}
	sort.Slice(d.Coverage.Categories, func(i, j int) bool { return d.Coverage.Categories[i].Name < d.Coverage.Categories[j].Name })
	d.Coverage.Includes = len(loadIncludes(env).Paths)
	d.Coverage.Uncovered = append([]string{}, uncovered(env)...)

	d.Backups = []dashBackup{}
	for _, b := range findBackups(env) {
		d.Backups = append(d.Backups, dashBackup{Path: shortHome(env, b.Path), Kind: b.Kind, Modified: b.Mod, Size: b.Size})
	}

	lg := restore.LoadLedger(ledgerPath(env))
	d.Ledger.Applied = len(lg.Applied)
	for _, s := range lg.Skipped {
		d.Ledger.Skipped += len(s)
	}
	for _, a := range lg.Applied {
		if d.Ledger.LastApplied == nil || a.At.After(*d.Ledger.LastApplied) {
			at := a.At
			d.Ledger.LastApplied = &at
		}
	}

	d.Inventory.Groups = []dashGroup{}
	if snap, src, date := newestInventory(env); snap != nil {
		d.Inventory.Source, d.Inventory.Date = src, date
		d.Inventory.Groups = inventoryGroups(snap)
	}
	return d
}

// newestInventory is the newest of: a `collect` snapshot, or the inventory
// inside the newest backup folder.
func newestInventory(env *sys.OS) (snapshot.Snapshot, string, string) {
	type cand struct {
		path, label string
		mod         time.Time
	}
	var cands []cand
	for _, p := range newestSnapshots(env, 1) {
		if fi, err := os.Stat(p); err == nil {
			cands = append(cands, cand{p, "snapshot " + filepath.Base(p), fi.ModTime()})
		}
	}
	for _, b := range findBackups(env) {
		if b.Kind != "folder" {
			continue
		}
		p := filepath.Join(b.Path, "inventory", "snapshot.json")
		if _, err := os.Stat(p); err == nil {
			cands = append(cands, cand{p, "backup " + filepath.Base(b.Path), b.Mod})
			break
		}
	}
	if len(cands) == 0 {
		return nil, "", ""
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].mod.After(cands[j].mod) })
	b, err := os.ReadFile(cands[0].path)
	if err != nil {
		return nil, "", ""
	}
	snap, err := snapshot.Parse(b)
	if err != nil {
		return nil, "", ""
	}
	return snap, cands[0].label, cands[0].mod.Format(time.RFC3339)
}

var inventoryLabels = map[string]string{
	"apps.brew.formulae": "Homebrew formulae", "apps.brew.casks": "Homebrew apps", "apps.macos": "Applications",
	"packages.npm.global": "npm globals", "packages.pnpm.global": "pnpm globals", "packages.bun.global": "bun globals",
	"packages.pipx": "pipx apps", "packages.uv": "uv tools", "packages.go.bin": "Go binaries",
	"runtimes.rust.crates": "cargo crates", "packages.node.fnm": "Node versions",
	"editor.vscode.extensions": "VS Code extensions", "editor.cursor.extensions": "Cursor extensions",
	"fonts.user": "Fonts you installed", "packages.apt": "apt packages", "packages.dnf": "dnf packages",
	"packages.pacman": "pacman packages", "packages.snap": "snaps", "packages.flatpak": "flatpaks",
	"packages.composer": "Composer globals", "packages.dotnet": ".NET tools", "packages.pub": "Dart globals",
	"mobile.ios.runtimes": "iOS simulator runtimes", "mobile.android.avds": "Android emulators",
}

func inventoryGroups(snap snapshot.Snapshot) []dashGroup {
	out := []dashGroup{}
	for id, sec := range snap {
		label, ok := inventoryLabels[id]
		if !ok || len(sec.Items) == 0 {
			continue
		}
		out = append(out, dashGroup{Label: label, Count: len(sec.Items)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Label < out[j].Label
	})
	return out
}

type dashSecretFile struct {
	Path     string `json:"path"`
	Severity string `json:"severity"`
	Label    string `json:"label"`
	Count    int    `json:"count"`
}

// dashSecrets names the files holding secrets and what kind — never the
// values, which is also why this panel exists rather than a file browser.
func dashSecrets(ctx context.Context, env *sys.OS) (any, error) {
	results, err := scanTracked(ctx, env, false)
	if err != nil {
		return nil, err
	}
	out := struct {
		Scanned int              `json:"scanned"`
		High    int              `json:"high"`
		Medium  int              `json:"medium"`
		Files   []dashSecretFile `json:"files"`
	}{Scanned: len(results), Files: []dashSecretFile{}}
	for _, r := range results {
		var top *scan.Finding
		n := 0
		for i, f := range r.Findings {
			if f.Pattern.Severity == scan.Low || f.Pattern.Action == scan.Include {
				continue
			}
			n++
			if top == nil || (f.Pattern.Severity == scan.High && top.Pattern.Severity != scan.High) {
				top = &r.Findings[i]
			}
		}
		if top == nil {
			continue
		}
		if top.Pattern.Severity == scan.High {
			out.High++
		} else {
			out.Medium++
		}
		out.Files = append(out.Files, dashSecretFile{Path: r.Path, Severity: string(top.Pattern.Severity), Label: top.Pattern.Label, Count: n})
	}
	sort.Slice(out.Files, func(i, j int) bool {
		if out.Files[i].Severity != out.Files[j].Severity {
			return out.Files[i].Severity == "HIGH"
		}
		return out.Files[i].Path < out.Files[j].Path
	})
	return out, nil
}

type dashRepo struct {
	Path     string `json:"path"`
	NoRemote bool   `json:"noRemote"`
	Detail   string `json:"detail"`
}

func dashRepos(ctx context.Context, env *sys.OS) any {
	found := gitwork.Find(ctx, []string{env.Home()}, 5)
	risky := gitwork.Inspect(ctx, gitwork.GitRunner, found, nil)
	out := struct {
		Checked int        `json:"checked"`
		Repos   []dashRepo `json:"repos"`
	}{Checked: len(found), Repos: []dashRepo{}}
	for _, r := range risky {
		if !r.AtRisk() {
			continue
		}
		var parts []string
		if r.Dirty > 0 {
			parts = append(parts, plural(r.Dirty, "uncommitted file"))
		}
		if r.Unsaved > 0 {
			parts = append(parts, plural(r.Unsaved, "unpushed commit"))
		}
		if r.Stashes > 0 {
			parts = append(parts, plural(r.Stashes, "stash"))
		}
		if len(r.Ignored) > 0 {
			parts = append(parts, plural(len(r.Ignored), "ignored secret file"))
		}
		out.Repos = append(out.Repos, dashRepo{Path: shortHome(env, r.Path), NoRemote: !r.HasRemote, Detail: strings.Join(parts, ", ")})
	}
	sort.Slice(out.Repos, func(i, j int) bool { return out.Repos[i].Path < out.Repos[j].Path })
	return out
}

func dashGitHub(ctx context.Context, env *sys.OS) any {
	type machine struct {
		Machine string `json:"machine"`
		Mode    string `json:"mode"`
		Created string `json:"created"`
	}
	out := struct {
		SignedIn bool      `json:"signedIn"`
		Login    string    `json:"login,omitempty"`
		Source   string    `json:"source,omitempty"`
		Repo     string    `json:"repo,omitempty"`
		Private  bool      `json:"private"`
		Machines []machine `json:"machines"`
	}{Machines: []machine{}}
	tok, src := resolveToken(ctx, env)
	if tok == "" {
		return out
	}
	c, me, err := githubClientNoPrompt(ctx, tok)
	if err != nil {
		return out
	}
	out.SignedIn, out.Login, out.Source = true, me.Login, src
	repo := firstNonEmpty(loadGitHubConfig(env).Repo, me.Login+"/"+defaultRepoName)
	r, err := c.GetRepo(ctx, repo)
	if err != nil {
		return out
	}
	out.Repo, out.Private = r.FullName, r.Private
	names, _ := c.Dir(ctx, repo, "machines")
	for _, m := range names {
		meta := readMachineMeta(ctx, c, repo, m)
		out.Machines = append(out.Machines, machine{m, meta.Mode, meta.Created})
	}
	return out
}
