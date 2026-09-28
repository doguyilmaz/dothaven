package cli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/doguyilmaz/dothaven/internal/backup"
	"github.com/doguyilmaz/dothaven/internal/github"
	"github.com/doguyilmaz/dothaven/internal/registry"
	"github.com/doguyilmaz/dothaven/internal/secretstore"
	"github.com/doguyilmaz/dothaven/internal/sys"
	"github.com/doguyilmaz/dothaven/internal/tui"
	"github.com/spf13/cobra"
)

// defaultRepoName is the private repository dothaven creates. Not "dotfiles":
// that is the name people already use for their own hand-kept repos, and
// dothaven must never write into one of those by accident.
const defaultRepoName = "dothaven-backup"

const (
	accountToken      = "github-token"
	accountPassphrase = "backup-passphrase"
	tokenEnv          = "DOTHAVEN_GITHUB_TOKEN"
)

// Backup modes for a GitHub push.
const (
	modeEncrypted = "encrypted" // one age-encrypted archive: everything, keys included
	modeSplit     = "split"     // readable config + an encrypted bundle of the sensitive files
	modePlain     = "plain"     // readable, secrets redacted, credentials left out
)

func configDir(env *sys.OS) string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "dothaven")
	}
	return filepath.Join(env.Home(), ".config", "dothaven")
}

func secrets(env *sys.OS) *secretstore.Store { return secretstore.Open(configDir(env)) }

type githubConfig struct {
	Repo   string `json:"repo,omitempty"`
	Branch string `json:"branch,omitempty"`
	Mode   string `json:"mode,omitempty"`
}

func githubConfigPath(env *sys.OS) string { return filepath.Join(configDir(env), "github.json") }

func loadGitHubConfig(env *sys.OS) githubConfig {
	var c githubConfig
	if b, err := os.ReadFile(githubConfigPath(env)); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	if c.Branch == "" {
		c.Branch = "main"
	}
	return c
}

func saveGitHubConfig(env *sys.OS, c githubConfig) error {
	b, _ := json.MarshalIndent(c, "", "  ")
	return sys.WriteFile(githubConfigPath(env), string(b)+"\n")
}

// resolveToken finds a GitHub token without asking: the environment, then
// dothaven's own stored one, then the GitHub CLI's login. It returns the token
// and where it came from, for `status`.
func resolveToken(ctx context.Context, env *sys.OS) (string, string) {
	if t := strings.TrimSpace(os.Getenv(tokenEnv)); t != "" {
		return t, tokenEnv
	}
	st := secrets(env)
	if t, err := st.Get(accountToken); err == nil && t != "" {
		return t, st.Name()
	}
	if _, err := exec.LookPath("gh"); err == nil {
		if t, err := runShell(ctx, "gh", "auth", "token"); err == nil && strings.TrimSpace(t) != "" {
			return strings.TrimSpace(t), "the GitHub CLI (gh)"
		}
	}
	return "", ""
}

var errNotSignedIn = errors.New("not signed in to GitHub — run: dothaven github login")

// githubClient returns a signed-in client, offering to sign in on a terminal.
func githubClient(ctx context.Context, env *sys.OS) (*github.Client, github.User, error) {
	tok, _ := resolveToken(ctx, env)
	if tok == "" {
		if !tui.Interactive() {
			return nil, github.User{}, errNotSignedIn
		}
		fmt.Println("You're not signed in to GitHub yet.")
		if err := githubLogin(ctx, env, false); err != nil {
			return nil, github.User{}, err
		}
		if tok, _ = resolveToken(ctx, env); tok == "" {
			return nil, github.User{}, errNotSignedIn
		}
	}
	c, err := github.New(tok)
	if err != nil {
		return nil, github.User{}, err
	}
	me, err := c.Me(ctx)
	if err != nil {
		var apiErr *github.APIError
		if errors.As(err, &apiErr) && apiErr.Status == 401 {
			return nil, github.User{}, errors.New("GitHub rejected the saved token (expired or revoked) — run: dothaven github login")
		}
		return nil, github.User{}, err
	}
	return c, me, nil
}

// githubClientNoPrompt is githubClient for callers that must never ask
// anything (the dashboard).
func githubClientNoPrompt(ctx context.Context, tok string) (*github.Client, github.User, error) {
	c, err := github.New(tok)
	if err != nil {
		return nil, github.User{}, err
	}
	me, err := c.Me(ctx)
	return c, me, err
}

func newGitHubCmd(env *sys.OS) *cobra.Command {
	c := &cobra.Command{
		Use:   "github",
		Short: "Keep your backup in a private GitHub repository",
		Long: "Pushes this machine's backup to a private repository on your GitHub account\n" +
			"(created for you as " + defaultRepoName + "), and restores from it on another machine —\n" +
			"no git needed on either side.\n\n" +
			"  dothaven github login       sign in (opens the browser), or reuse your gh login\n" +
			"  dothaven github push        back this machine up to the repo\n" +
			"  dothaven restore github     on a new machine: restore from the repo\n" +
			"  dothaven github status      who you are, which repo, which machines are in it\n\n" +
			"Three ways to store it (--mode):\n" +
			"  encrypted   one age-encrypted file with everything, keys included (default)\n" +
			"  split       readable config files; credentials and anything with a secret in\n" +
			"              it go in an encrypted bundle beside them\n" +
			"  plain       readable, secrets redacted, credential files left out\n\n" +
			"dothaven refuses to write to a public repository.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return githubStatus(cmd.Context(), env) },
	}
	c.AddCommand(newGitHubLoginCmd(env), newGitHubLogoutCmd(env), newGitHubStatusCmd(env),
		newGitHubPushCmd(env), newGitHubPullCmd(env))
	return c
}

func newGitHubLoginCmd(env *sys.OS) *cobra.Command {
	var withToken bool
	c := &cobra.Command{
		Use:   "login",
		Short: "Sign in to GitHub (browser, gh CLI, or a token on stdin)",
		Long: "Opens github.com in your browser with a one-time code; approve it and the\n" +
			"terminal carries on by itself. The token is kept in your system keychain.\n\n" +
			"Already signed in to the GitHub CLI (gh)? dothaven uses that login and stores\n" +
			"nothing of its own.\n\n" +
			"Most locked down: a fine-grained token that can only touch the one repository\n" +
			"(Contents: read & write, Administration: read & write to create it):\n" +
			"  dothaven github login --with-token < token.txt",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return githubLogin(cmd.Context(), env, withToken)
		},
	}
	c.Flags().BoolVar(&withToken, "with-token", false, "read a token from stdin instead of opening the browser")
	return c
}

func githubLogin(ctx context.Context, env *sys.OS, withToken bool) error {
	st := secrets(env)
	save := func(tok string) error {
		c, err := github.New(tok)
		if err != nil {
			return err
		}
		me, err := c.Me(ctx)
		if err != nil {
			return fmt.Errorf("GitHub did not accept that token: %w", err)
		}
		if err := st.Set(accountToken, tok); err != nil {
			return fmt.Errorf("could not save the token: %w", err)
		}
		fmt.Printf("%s Signed in as %s. Token kept in %s.\n", good("✓"), bold(me.Login), st.Name())
		return nil
	}

	if withToken {
		b := make([]byte, 4096)
		n, _ := os.Stdin.Read(b)
		tok := strings.TrimSpace(string(b[:n]))
		if tok == "" {
			return errors.New("no token on stdin")
		}
		return save(tok)
	}

	clientID := github.ClientIDFromEnv()
	if clientID == "" {
		if _, err := exec.LookPath("gh"); err == nil {
			if t, err := runShell(ctx, "gh", "auth", "token"); err == nil && strings.TrimSpace(t) != "" {
				fmt.Printf("%s Using your GitHub CLI login — dothaven stores nothing of its own.\n", good("✓"))
				return nil
			}
			fmt.Println("Sign in with the GitHub CLI first — dothaven will use that login:")
			fmt.Printf("  %s\n", kbd("gh auth login"))
			return ExitError{Code: 1}
		}
		fmt.Println("This build has no GitHub app configured for browser sign-in. Either:")
		fmt.Printf("  • install the GitHub CLI and sign in:  %s\n", kbd("brew install gh && gh auth login"))
		fmt.Printf("  • or create a fine-grained token at https://github.com/settings/personal-access-tokens/new\n")
		fmt.Printf("    (one repository, Contents + Administration read & write) and run:\n    %s\n", kbd("dothaven github login --with-token < token.txt"))
		return ExitError{Code: 1}
	}

	c, err := github.New("")
	if err != nil {
		return err
	}
	dc, err := c.StartDeviceFlow(ctx, clientID)
	if err != nil {
		return err
	}
	fmt.Printf("\n  1. Your one-time code:  %s\n", bold(dc.UserCode))
	fmt.Printf("  2. Enter it at:         %s\n\n", kbd(dc.VerificationURI))
	copyToClipboard(dc.UserCode)
	openBrowser(dc.VerificationURI)
	fmt.Println(dim("  Opened your browser (the code is on your clipboard). Waiting for you to approve…"))
	tok, err := c.WaitForToken(ctx, clientID, dc)
	if err != nil {
		return err
	}
	return save(tok)
}

// openBrowser opens a URL without waiting for the browser.
func openBrowser(u string) {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	if _, err := exec.LookPath(name); err != nil {
		return
	}
	cmd := exec.Command(name, u)
	if cmd.Start() == nil {
		go func() { _ = cmd.Wait() }()
	}
}

// copyToClipboard is best effort, and bounded: a clipboard daemon that does
// not answer must not hold up sign-in.
func copyToClipboard(s string) {
	for _, c := range [][]string{{"pbcopy"}, {"wl-copy"}, {"xclip", "-selection", "clipboard"}} {
		if _, err := exec.LookPath(c[0]); err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		cmd := exec.CommandContext(ctx, c[0], c[1:]...)
		cmd.Stdin = strings.NewReader(s)
		cmd.WaitDelay = time.Second
		_ = cmd.Run()
		cancel()
		return
	}
}

func newGitHubLogoutCmd(env *sys.OS) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Forget the stored GitHub token and remembered passphrase",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			st := secrets(env)
			_ = st.Delete(accountToken)
			_ = st.Delete(accountPassphrase)
			fmt.Printf("%s Removed dothaven's GitHub token and remembered passphrase from %s.\n", good("✓"), st.Name())
			if _, src := resolveToken(cmd.Context(), env); src != "" {
				fmt.Printf("  %s still signed in through %s.\n", dim("•"), src)
			}
			return nil
		},
	}
}

func newGitHubStatusCmd(env *sys.OS) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Who you're signed in as, which repo, and which machines are in it",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return githubStatus(cmd.Context(), env) },
	}
}

type machineMeta struct {
	Machine  string `json:"machine"`
	Host     string `json:"host"`
	OS       string `json:"os"`
	Mode     string `json:"mode"`
	Created  string `json:"created"`
	Files    int    `json:"files"`
	Dothaven string `json:"dothaven"`
	// Fingerprint is a hash of what was backed up (not of the encrypted
	// bytes), so an unchanged machine is recognised and not pushed again.
	Fingerprint string `json:"fingerprint"`
	// AgeHeader is the header of the encrypted file, base64: enough to tell
	// whether this push's passphrase is the one that copy was made with. It
	// reveals nothing the encrypted file beside it does not.
	AgeHeader string `json:"ageHeader,omitempty"`
}

func githubStatus(ctx context.Context, env *sys.OS) error {
	tok, src := resolveToken(ctx, env)
	if tok == "" {
		fmt.Println("Not signed in to GitHub.")
		if !tui.Interactive() {
			fmt.Printf("  %s\n", kbd("dothaven github login"))
			return nil
		}
		if ok, err := tui.Confirm("Sign in now? (opens your browser)"); err != nil || !ok {
			return ignoreAbort(err)
		}
		if err := githubLogin(ctx, env, false); err != nil {
			return err
		}
		if tok, src = resolveToken(ctx, env); tok == "" {
			return nil
		}
	}
	c, me, err := githubClient(ctx, env)
	if err != nil {
		return err
	}
	cfg := loadGitHubConfig(env)
	repo := cfg.Repo
	if repo == "" {
		repo = me.Login + "/" + defaultRepoName
	}
	fmt.Printf("%s %s %s\n", bold("Signed in as"), me.Login, dim("(via "+src+")"))
	r, err := c.GetRepo(ctx, repo)
	if errors.Is(err, github.ErrNotFound) {
		fmt.Printf("%s %s %s\n", bold("Repository:"), repo, dim("— not created yet; `dothaven github push` creates it (private)"))
		return nil
	}
	if err != nil {
		return err
	}
	vis := good("private")
	if !r.Private {
		vis = danger("PUBLIC — dothaven will not write to it")
	}
	fmt.Printf("%s %s (%s)\n", bold("Repository:"), r.HTMLURL, vis)
	machines, _ := c.Dir(ctx, repo, "machines")
	if len(machines) == 0 {
		fmt.Println("  No machines backed up yet.")
		return nil
	}
	fmt.Println(bold("Machines:"))
	for _, m := range machines {
		meta := readMachineMeta(ctx, c, repo, m)
		here := ""
		if m == machineName() {
			here = good(" ← this one")
		}
		fmt.Printf("  %s %s%s\n", padTo(m, 24), dim(fmt.Sprintf("%s, %s, %s", meta.Mode, meta.OS, shortDate(meta.Created))), here)
	}
	return nil
}

func readMachineMeta(ctx context.Context, c *github.Client, repo, m string) machineMeta {
	var meta machineMeta
	if b, err := c.Raw(ctx, repo, "machines/"+m+"/dothaven.json"); err == nil {
		_ = json.Unmarshal(b, &meta)
	}
	if meta.Mode == "" {
		meta.Mode = "?"
	}
	return meta
}

func shortDate(rfc string) string {
	if t, err := time.Parse(time.RFC3339, rfc); err == nil {
		return t.Local().Format("2 Jan 2006 15:04")
	}
	return rfc
}

var unsafeName = regexp.MustCompile(`[^a-z0-9._-]+`)

// machineName is this machine's folder in the repo: the hostname, lowercased
// and made safe for a path.
func machineName() string {
	n := strings.Trim(unsafeName.ReplaceAllString(strings.ToLower(hostname()), "-"), "-.")
	if n == "" {
		n = "machine"
	}
	return n
}

func newGitHubPushCmd(env *sys.OS) *cobra.Command {
	var mode, repo, machine string
	var only, skip []string
	var assumeYes bool
	c := &cobra.Command{
		Use:     "push",
		Aliases: []string{"sync", "save"},
		Short:   "Back this machine up to your private GitHub repo",
		Long: "Builds a backup of this machine (the same one `dothaven backup` makes) and\n" +
			"commits it to machines/<this machine>/ in your private repository, replacing\n" +
			"the previous one there. Other machines in the repo are left alone, and git\n" +
			"history keeps every earlier push. Nothing changes if nothing changed.",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return githubPush(cmd, env, pushOpts{mode: mode, repo: repo, machine: machine, only: only, skip: skip, yes: assumeYes})
		},
	}
	c.Flags().StringVar(&mode, "mode", "", "encrypted (default), split, or plain — see `dothaven github --help`")
	c.Flags().StringVar(&repo, "repo", "", "owner/name (default: <you>/"+defaultRepoName+")")
	c.Flags().StringVar(&machine, "machine", "", "folder name for this machine in the repo (default: hostname)")
	c.Flags().StringSliceVar(&only, "only", nil, "only these categories")
	c.Flags().StringSliceVar(&skip, "skip", nil, "skip these categories")
	c.Flags().BoolVar(&assumeYes, "yes", false, "create the repository without asking (required off a terminal)")
	return c
}

type pushOpts struct {
	mode, repo, machine string
	only, skip          []string
	yes                 bool
}

func githubPush(cmd *cobra.Command, env *sys.OS, o pushOpts) error {
	ctx := cmd.Context()
	c, me, err := githubClient(ctx, env)
	if err != nil {
		return err
	}
	cfg := loadGitHubConfig(env)
	repo := firstNonEmpty(o.repo, cfg.Repo, me.Login+"/"+defaultRepoName)
	machine := firstNonEmpty(o.machine, machineName())
	if unsafeName.MatchString(machine) {
		return fmt.Errorf("machine name %q: use lowercase letters, digits, '.', '-' or '_'", machine)
	}

	r, err := c.GetRepo(ctx, repo)
	switch {
	case errors.Is(err, github.ErrNotFound):
		owner, name, _ := strings.Cut(repo, "/")
		if owner != me.Login {
			return fmt.Errorf("%s does not exist (dothaven only creates repositories on your own account)", repo)
		}
		if err := confirmWrite(os.Stderr, fmt.Sprintf("Create the private repository %s?", repo), o.yes); err != nil {
			return err
		}
		if r, err = c.CreatePrivateRepo(ctx, name, "Private backup of my dev setup, made by dothaven — keep this private."); err != nil {
			return fmt.Errorf("could not create %s: %w", repo, err)
		}
		fmt.Printf("%s Created %s (private)\n", good("✓"), r.HTMLURL)
	case err != nil:
		return err
	}
	// The line that does not move: even an encrypted backup says which
	// services you use, and a plain one is your config for anyone to read.
	if !r.Private {
		return fmt.Errorf("%s is PUBLIC — dothaven only writes to private repositories. Make it private on GitHub, or pick another with --repo", r.FullName)
	}

	mode := firstNonEmpty(o.mode, cfg.Mode)
	if mode == "" {
		mode = modeEncrypted
		if tui.Interactive() {
			if mode, err = askPushMode(); err != nil {
				return ignoreAbort(err)
			}
		}
	}
	if mode != modeEncrypted && mode != modeSplit && mode != modePlain {
		return fmt.Errorf("unknown --mode %q: use encrypted, split or plain", mode)
	}

	pass := ""
	if mode != modePlain {
		if pass, err = pushPassphrase(env); err != nil {
			return err
		}
	}

	tmp, err := os.MkdirTemp("", "dothaven-push-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := os.Chmod(tmp, 0o700); err != nil {
		return err
	}

	digest := &backup.DigestSink{}
	files, out, err := buildPushFiles(ctx, cmd, env, tmp, mode, pass, o.only, o.skip, digest)
	if err != nil {
		return err
	}
	// Encrypted means encrypted: check the bytes about to leave this machine,
	// not the flag that asked for them.
	var header []byte
	for _, f := range files {
		if !strings.HasSuffix(f.Path, ".age") {
			continue
		}
		if backup.Detect(f.Src) != backup.FormatAge {
			return fmt.Errorf("refusing to upload: %s is not encrypted", f.Path)
		}
		if header == nil {
			if header, err = backup.AgeHeader(f.Src); err != nil {
				return err
			}
		}
	}
	// The version is part of the fingerprint: a newer dothaven may carry more,
	// or classify a file as sensitive that an older one left readable.
	fp := mode + ":" + cmd.Root().Version + ":" + digest.Sum()
	if remote := readMachineMeta(ctx, c, r.FullName, machine); remote.Fingerprint == fp {
		if samePassphrase(remote, header, pass) {
			cfg.Repo, cfg.Mode = r.FullName, mode
			_ = saveGitHubConfig(env, cfg)
			fmt.Printf("%s Already up to date — nothing changed since the last push (%s).\n", good("✓"), shortDate(remote.Created))
			return nil
		}
		fmt.Println(dim("Nothing changed, but the copy on GitHub was made with a different passphrase — replacing it with one yours opens."))
	}
	var headerB64 string
	if header != nil {
		headerB64 = base64.StdEncoding.EncodeToString(header)
	}
	meta, _ := json.MarshalIndent(machineMeta{
		Machine: machine, Host: hostname(), OS: runtime.GOOS, Mode: mode,
		Created: time.Now().UTC().Format(time.RFC3339), Files: out.res.TotalFiles, Dothaven: cmd.Root().Version,
		Fingerprint: fp, AgeHeader: headerB64,
	}, "", "  ")
	metaPath := filepath.Join(tmp, "dothaven.json")
	if err := os.WriteFile(metaPath, append(meta, '\n'), 0o600); err != nil {
		return err
	}
	files = append(files, github.File{Path: "dothaven.json", Src: metaPath, Size: int64(len(meta) + 1)})

	var total int64
	for _, f := range files {
		total += f.Size
	}
	fmt.Printf("\nUploading %s (%s) to %s …\n", plural(len(files), "file"), humanBytes(total), r.FullName)
	sha, err := c.Commit(ctx, r.FullName, firstNonEmpty(r.DefaultBranch, cfg.Branch), "machines/"+machine, files,
		map[string]string{"README.md": repoReadme(r.FullName)},
		fmt.Sprintf("dothaven: %s — %s backup, %s", machine, mode, plural(out.res.TotalFiles, "file")))
	if err != nil {
		if ctx.Err() != nil {
			return ExitError{Code: 130}
		}
		return err
	}
	cfg.Repo, cfg.Mode = r.FullName, mode
	if err := saveGitHubConfig(env, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "  %s could not save your repo choice: %v\n", warn("⚠"), err)
	}
	if sha == "" {
		fmt.Printf("%s Already up to date — nothing changed since the last push.\n", good("✓"))
		return nil
	}
	fmt.Printf("%s Pushed %s as machines/%s %s\n", good("✓"), plural(out.res.TotalFiles, "file"), machine, dim("("+mode+", commit "+sha[:7]+")"))
	fmt.Printf("  %s\n", r.HTMLURL+"/tree/"+firstNonEmpty(r.DefaultBranch, "main")+"/machines/"+machine)
	printLeftOut(out.res, mode != modePlain, "dothaven github push --mode encrypted")
	fmt.Println("\n" + bold("On the new machine:"))
	fmt.Printf("  %s\n  %s\n", kbd("dothaven github login"), kbd("dothaven restore github"))
	if mode != modePlain {
		fmt.Println(dim("  You will need the passphrase. Nothing can open the encrypted part without it."))
	}
	return nil
}

// samePassphrase reports whether the copy on GitHub opens with pass. With
// nothing encrypted in this push there is nothing to open, so any passphrase
// is the same one; with no header recorded, it cannot be known and the push
// goes ahead.
func samePassphrase(remote machineMeta, header []byte, pass string) bool {
	if header == nil {
		return true
	}
	hdr, err := base64.StdEncoding.DecodeString(remote.AgeHeader)
	if err != nil || len(hdr) == 0 {
		return false
	}
	return backup.HeaderOpens(hdr, pass)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func askPushMode() (string, error) {
	return tui.Ask("How should it be stored?", "You can change this on any later push with --mode.", []tui.Choice{
		{Label: "Encrypted — everything, keys included", Value: modeEncrypted, Hint: "one file only your passphrase opens (recommended)"},
		{Label: "Readable, with secrets encrypted", Value: modeSplit, Hint: "browse and diff config on GitHub; credentials in an encrypted bundle"},
		{Label: "Readable, secrets redacted", Value: modePlain, Hint: "no passphrase; SSH keys and logins are NOT included"},
	})
}

// pushPassphrase is the passphrase for the encrypted part, remembered in the
// keychain after the first push so a routine push doesn't ask every time.
// The keychain is local; the passphrase protects the copy on GitHub.
func pushPassphrase(env *sys.OS) (string, error) {
	if p, ok, err := envPassphrase(); ok {
		return p, err
	}
	st := secrets(env)
	if p, err := st.Get(accountPassphrase); err == nil && len([]rune(p)) >= minPassphrase {
		fmt.Println(dim("Using your remembered backup passphrase (forget it with `dothaven github logout`)."))
		return p, nil
	}
	p, err := newPassphrase()
	if err != nil {
		return "", err
	}
	if tui.Interactive() {
		if ok, _ := tui.Confirm("Remember this passphrase on this machine (in " + st.Name() + ") so later pushes don't ask?"); ok {
			if err := st.Set(accountPassphrase, p); err != nil {
				fmt.Fprintf(os.Stderr, "  %s could not remember it: %v\n", warn("⚠"), err)
			}
		}
	}
	return p, nil
}

// buildPushFiles writes this machine's backup into tmp in the chosen mode and
// returns the files to commit.
func buildPushFiles(ctx context.Context, cmd *cobra.Command, env *sys.OS, tmp, mode, pass string, only, skip []string, digest *backup.DigestSink) ([]github.File, backupOutcome, error) {
	host := hostname()
	name := fmt.Sprintf("backup-%s-%s", host, sys.Timestamp(time.Now()))
	targets := registry.BackupTargets(env.Home(), allEntries(env))
	if err := validateCategories(targets, only, skip, catInventory, catMacOS, registry.ExtraCategory); err != nil {
		return nil, backupOutcome{}, err
	}
	var out backupOutcome
	switch mode {
	case modeEncrypted:
		o := backupOpts{output: tmp, archive: true, encrypt: true, passphrase: pass, only: only, skip: skip, digest: digest, remote: true}
		res, err := runBackup(ctx, cmd, env, o)
		if err != nil {
			return nil, res, err
		}
		fi, err := os.Stat(res.path)
		if err != nil {
			return nil, res, err
		}
		if fi.Size() > github.MaxFile {
			return nil, res, fmt.Errorf("the encrypted backup is %s; GitHub takes 100 MB per file at most — leave out something large with --skip, or use --mode split", humanBytes(fi.Size()))
		}
		return []github.File{{Path: "backup.tar.gz.age", Src: res.path, Size: fi.Size()}}, res, nil

	case modePlain:
		tree := filepath.Join(tmp, "tree")
		o := backupOpts{only: only, skip: skip, digest: digest, remote: true}
		if err := fillBackup(ctx, cmd, env, o, targets, backup.DirSink{Root: tree}, host, &out); err != nil {
			return nil, out, backupErr(err)
		}
		files, err := treeFiles(tree)
		return files, out, err

	default: // split
		tree := filepath.Join(tmp, "tree")
		secretsPath := filepath.Join(tmp, "secrets.tar.gz.age")
		var split *backup.SplitSink
		o := backupOpts{noRedact: true, split: true, only: only, skip: skip, digest: digest, remote: true}
		err := backup.WriteEncryptedArchive(secretsPath, name, pass, func(secret backup.Sink) error {
			split = &backup.SplitSink{Plain: backup.DirSink{Root: tree}, Secret: secret}
			return fillBackup(ctx, cmd, env, o, targets, split, host, &out)
		})
		if err != nil {
			return nil, out, backupErr(err)
		}
		files, err := treeFiles(tree)
		if err != nil {
			return nil, out, err
		}
		fmt.Printf("  %s readable, %s encrypted\n", plural(len(files), "file"), plural(split.Secrets, "file"))
		if split.Secrets > 0 {
			fi, err := os.Stat(secretsPath)
			if err != nil {
				return nil, out, err
			}
			files = append(files, github.File{Path: "secrets.tar.gz.age", Src: secretsPath, Size: fi.Size()})
		}
		return files, out, nil
	}
}

func backupErr(err error) error {
	if errors.Is(err, backup.ErrNothingToWrite) {
		return fmt.Errorf("nothing to back up — no tracked files found for this selection")
	}
	return err
}

// treeFiles lists a backup tree as commit files. A nested .git directory
// cannot be committed (git refuses a path component named .git), so those are
// left out of readable pushes; the encrypted mode carries them inside its
// archive.
func treeFiles(root string) ([]github.File, error) {
	var out []github.File
	skipped := 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				skipped++
				return fs.SkipDir
			}
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		out = append(out, github.File{Path: filepath.ToSlash(rel), Src: p, Size: fi.Size(), Exec: fi.Mode().Perm()&0o111 != 0})
		return nil
	})
	if skipped > 0 {
		fmt.Printf("  %s %s left out of the readable copy (git cannot store a .git inside a repository; --mode encrypted keeps them)\n", dim("•"), plural(skipped, ".git folder"))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, err
}

func repoReadme(full string) string {
	return "# dothaven backup\n\n" +
		"A private backup of my dev setup — dotfiles, editor and AI-tool config, app list —\n" +
		"written by [dothaven](https://github.com/doguyilmaz/dothaven). Each machine has a folder under `machines/`.\n\n" +
		"**Keep this repository private.** Encrypted parts (`*.age`) open only with the passphrase\n" +
		"chosen when they were pushed; git history keeps every earlier push.\n\n" +
		"## Restore on a new machine\n\n" +
		"```sh\n" +
		"brew install --cask doguyilmaz/tap/dothaven\n" +
		"dothaven github login\n" +
		"dothaven restore github\n" +
		"```\n"
}

func newGitHubPullCmd(env *sys.OS) *cobra.Command {
	var repo, machine string
	var o restoreOpts
	c := &cobra.Command{
		Use:     "pull",
		Aliases: []string{"restore"},
		Short:   "Restore from your GitHub repo (same as `dothaven restore github`)",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			spec := "github"
			if repo != "" {
				spec += ":" + repo
			}
			if machine != "" {
				spec += "#" + machine
			}
			return runRestore(cmd, env, spec, o)
		},
	}
	c.Flags().StringVar(&repo, "repo", "", "owner/name (default: the one you pushed to, or <you>/"+defaultRepoName+")")
	c.Flags().StringVar(&machine, "machine", "", "which machine's backup (default: ask, or the only one)")
	c.Flags().BoolVar(&o.dryRun, "dry-run", false, "show what would change without writing")
	c.Flags().BoolVar(&o.force, "force", false, "overwrite differing files (a pre-restore snapshot is saved first)")
	c.Flags().BoolVar(&o.yes, "yes", false, "don't ask before writing")
	return c
}

// isGitHubSpec reports whether a backup argument names the GitHub repo:
// "github", "github:owner/repo", with an optional "#machine".
func isGitHubSpec(s string) bool {
	return s == "github" || strings.HasPrefix(s, "github:") || strings.HasPrefix(s, "github#")
}

// absBackupArg makes a path absolute, leaving a GitHub spec alone.
func absBackupArg(a string) (string, error) {
	if isGitHubSpec(a) {
		return a, nil
	}
	return filepath.Abs(a)
}

// openGitHubBackup downloads a machine's backup from the repo and opens it
// like a local one. The download is the repository tarball (no git needed),
// unpacked into a private temporary directory; an encrypted part asks for the
// passphrase. The directory is named for the repo and machine, so the apply
// ledger recognises the same backup on the next pull.
func openGitHubBackup(ctx context.Context, env *sys.OS, spec string) (string, func(), error) {
	noop := func() {}
	rest := strings.TrimPrefix(strings.TrimPrefix(spec, "github"), ":")
	repo, machine, _ := strings.Cut(rest, "#")

	c, me, err := githubClient(ctx, env)
	if err != nil {
		return "", noop, err
	}
	repo = firstNonEmpty(repo, loadGitHubConfig(env).Repo, me.Login+"/"+defaultRepoName)
	r, err := c.GetRepo(ctx, repo)
	if err != nil {
		if errors.Is(err, github.ErrNotFound) {
			return "", noop, fmt.Errorf("no repository %s (or this login cannot see it) — push from the old machine first: dothaven github push", repo)
		}
		return "", noop, err
	}
	machines, err := c.Dir(ctx, repo, "machines")
	if err != nil || len(machines) == 0 {
		return "", noop, fmt.Errorf("%s has no machine backups yet — run `dothaven github push` on the old machine", repo)
	}
	if machine == "" {
		switch {
		case len(machines) == 1:
			machine = machines[0]
		case tui.Interactive():
			var choices []tui.Choice
			for _, m := range machines {
				meta := readMachineMeta(ctx, c, repo, m)
				choices = append(choices, tui.Choice{Label: m, Value: m, Hint: fmt.Sprintf("%s · %s · %s", meta.Mode, meta.OS, shortDate(meta.Created))})
			}
			if machine, err = tui.Ask("Which machine's backup?", repo, choices); err != nil {
				return "", noop, err
			}
		default:
			return "", noop, fmt.Errorf("%s holds several machines (%s) — pick one: dothaven github pull --machine <name>", repo, strings.Join(machines, ", "))
		}
	} else if !contains(machines, machine) {
		return "", noop, fmt.Errorf("no machine %q in %s (have: %s)", machine, repo, strings.Join(machines, ", "))
	}

	tmp, err := os.MkdirTemp("", "dothaven-github-")
	if err != nil {
		return "", noop, err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	fail := func(err error) (string, func(), error) { cleanup(); return "", noop, err }
	if err := os.Chmod(tmp, 0o700); err != nil {
		return fail(err)
	}

	fmt.Printf("Downloading %s from %s …\n", machine, repo)
	tarPath := filepath.Join(tmp, "repo.tar.gz")
	f, err := os.OpenFile(tarPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return fail(err)
	}
	err = c.Tarball(ctx, repo, firstNonEmpty(r.DefaultBranch, "main"), f)
	f.Close()
	if err != nil {
		return fail(err)
	}
	root, err := backup.Extract(tarPath, filepath.Join(tmp, "repo"))
	os.Remove(tarPath)
	if err != nil {
		return fail(err)
	}
	mdir := filepath.Join(root, "machines", machine)

	// Encrypted mode: the machine folder holds one archive; open it as any
	// local encrypted backup (with its passphrase retries).
	if archive := filepath.Join(mdir, "backup.tar.gz.age"); fileExists(archive) {
		dir, inner, err := openBackup(ctx, env, archive)
		if err != nil {
			return fail(err)
		}
		return dir, func() { inner(); cleanup() }, nil
	}

	stable := filepath.Join(tmp, "github-"+strings.ReplaceAll(repo, "/", "-")+"-"+machine)
	if err := os.Rename(mdir, stable); err != nil {
		return fail(err)
	}
	// Split mode: merge the encrypted bundle back into the readable files.
	if secretsPath := filepath.Join(stable, "secrets.tar.gz.age"); fileExists(secretsPath) {
		sdir, inner, err := openBackup(ctx, env, secretsPath)
		if err != nil {
			return fail(err)
		}
		err = filepath.WalkDir(sdir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(sdir, p)
			dst := filepath.Join(stable, rel)
			if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
				return err
			}
			return os.Rename(p, dst)
		})
		inner()
		if err != nil {
			return fail(err)
		}
	}
	return stable, cleanup, nil
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}
