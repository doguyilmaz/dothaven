package cli

import (
	"cmp"
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
//
// A GitHub App sign-in lasts 8 hours. With renew set, a stale one is renewed
// with its refresh token and the new pair saved; the read-only callers (the
// dashboard, doctor) pass false and get errRenewDue instead, because a
// refresh token works once and a renewed pair that is not kept is lost.
func resolveToken(ctx context.Context, env *sys.OS, renew bool) (string, string, error) {
	if t, _ := lookupSecretEnv(tokenEnv); strings.TrimSpace(t) != "" {
		return strings.TrimSpace(t), tokenEnv, nil
	}
	st := secrets(env)
	if s, err := st.Get(accountToken); err == nil && s != "" {
		t := decodeToken(s)
		switch {
		case !t.Stale(time.Now()) || t.Refresh == "":
			return t.Access, st.Name(), nil
		case !renew:
			return t.Access, st.Name(), errRenewDue
		}
		t, err = renewToken(ctx, st, t)
		return t.Access, st.Name(), err
	}
	if _, err := exec.LookPath("gh"); err == nil {
		if t, err := runShell(ctx, "gh", "auth", "token"); err == nil && strings.TrimSpace(t) != "" {
			return strings.TrimSpace(t), "the GitHub CLI (gh)", nil
		}
	}
	return "", "", nil
}

var (
	errNotSignedIn = errors.New("not signed in to GitHub — run: dothaven github login")
	errRenewDue    = errors.New("the GitHub sign-in is due for renewal — any dothaven github command renews it: dothaven github status")
)

// encodeToken is how a sign-in is kept: a bare token when it does not expire
// (what --with-token and older versions store), JSON when it carries a
// refresh token.
func encodeToken(t github.Token) string {
	if t.Refresh == "" {
		return t.Access
	}
	b, _ := json.Marshal(t)
	return string(b)
}

func decodeToken(s string) github.Token {
	var t github.Token
	if strings.HasPrefix(s, "{") && json.Unmarshal([]byte(s), &t) == nil && t.Access != "" {
		return t
	}
	return github.Token{Access: s}
}

// renewToken trades a stale GitHub App token for a fresh pair and keeps it.
// GitHub replaces the refresh token on every use, so when two runs renew at
// once the second finds its refresh token spent — and the first one's new
// pair already stored, which it then uses.
func renewToken(ctx context.Context, st *secretstore.Store, t github.Token) (github.Token, error) {
	clientID := github.ClientIDFromEnv()
	if clientID == "" {
		return t, github.ErrSignInExpired
	}
	c, err := github.New("")
	if err != nil {
		return t, err
	}
	nt, err := c.RefreshToken(ctx, clientID, t.Refresh)
	if errors.Is(err, github.ErrSignInExpired) {
		if s, gerr := st.Get(accountToken); gerr == nil {
			if cur := decodeToken(s); cur.Refresh != t.Refresh && !cur.Stale(time.Now()) {
				return cur, nil
			}
		}
		return t, err
	}
	if err != nil {
		return t, fmt.Errorf("could not renew the GitHub sign-in: %w", err)
	}
	if err := st.Set(accountToken, encodeToken(nt)); err != nil {
		fmt.Fprintf(os.Stderr, "%s Renewed the GitHub sign-in but could not save it (%v); the next run will ask you to sign in again.\n", warn("⚠"), err)
	}
	return nt, nil
}

// githubClient returns a signed-in client, offering to sign in on a terminal.
func githubClient(ctx context.Context, env *sys.OS) (*github.Client, github.User, error) {
	tok, _, err := resolveToken(ctx, env, true)
	expired := errors.Is(err, github.ErrSignInExpired)
	if err != nil && !expired {
		return nil, github.User{}, err
	}
	if tok == "" || expired {
		if !tui.Interactive() {
			return nil, github.User{}, cmp.Or(err, errNotSignedIn)
		}
		if expired {
			fmt.Println("Your GitHub sign-in has expired.")
		} else {
			fmt.Println("You're not signed in to GitHub yet.")
		}
		if err := githubLogin(ctx, env, false); err != nil {
			return nil, github.User{}, err
		}
		if tok, _, err = resolveToken(ctx, env, true); err != nil || tok == "" {
			return nil, github.User{}, cmp.Or(err, errNotSignedIn)
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
	save := func(tok github.Token) error {
		c, err := github.New(tok.Access)
		if err != nil {
			return err
		}
		me, err := c.Me(ctx)
		if err != nil {
			return fmt.Errorf("GitHub did not accept that token: %w", err)
		}
		if err := st.Set(accountToken, encodeToken(tok)); err != nil {
			return fmt.Errorf("could not save the token: %w", err)
		}
		fmt.Printf("%s Signed in as %s. Token kept in %s.\n", good("✓"), bold(me.Login), st.Name())
		if github.IsAppToken(tok.Access) {
			if n, err := c.Installations(ctx); err == nil && n == 0 {
				fmt.Printf("\n  %s the dothaven app can't reach any repository yet. Make a private repository\n"+
					"  named %s (https://github.com/new), then install the app on it:\n  %s\n",
					bold("One more step:"), defaultRepoName, kbd(appInstallURL()))
			}
		}
		return nil
	}

	if withToken {
		b := make([]byte, 4096)
		n, _ := os.Stdin.Read(b)
		tok := strings.TrimSpace(string(b[:n]))
		if tok == "" {
			return errors.New("no token on stdin")
		}
		return save(github.Token{Access: tok})
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
			if _, src, _ := resolveToken(cmd.Context(), env, false); src != "" {
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
	tok, src, err := resolveToken(ctx, env, true)
	expired := errors.Is(err, github.ErrSignInExpired)
	if err != nil && !expired {
		return err
	}
	if tok == "" || expired {
		if expired {
			fmt.Println("Your GitHub sign-in has expired.")
		} else {
			fmt.Println("Not signed in to GitHub.")
		}
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
		if tok, src, err = resolveToken(ctx, env, true); err != nil || tok == "" {
			return err
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
	if g, ok := loadGitSigning(ctx); ok {
		fmt.Printf("%s %s %s\n", bold("Commits:"), "signed with "+g.describe(), dim("(from your git config)"))
	} else {
		fmt.Printf("%s %s\n", bold("Commits:"), dim("unsigned; for GitHub's Verified badge, have git sign commits (GitHub sync docs → Verified commits)"))
	}
	r, err := c.GetRepo(ctx, repo)
	if errors.Is(err, github.ErrNotFound) {
		if h := appAccessHint(tok, repo); h != "" {
			fmt.Printf("%s %s %s\n%s\n", bold("Repository:"), repo, dim("— not created yet, or the dothaven app has no access to it."), dim(strings.TrimPrefix(h, "\n")))
			return nil
		}
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

// signCommits sets who each push names, as lockstep's vault does: the
// build's GitHub App bot as author (its avatar on the history), and the
// signed-in account as committer, by its noreply address so a push never
// publishes a real email. When the user's git signs commits, the push is
// signed with the same key; GitHub checks that against the committer, so it
// shows Verified once the key is one of the account's signing keys.
func signCommits(ctx context.Context, env *sys.OS, c *github.Client, me github.User) (gitSigning, bool) {
	if me.ID == 0 {
		return gitSigning{}, false
	}
	you := github.NoReply(me.Login, me.ID)
	c.Author, c.Committer = &you, &you
	if slug := github.AppSlugFromEnv(); slug != "" {
		if bot, err := c.Bot(ctx, slug); err == nil {
			c.Author = &bot
		}
	}
	g, ok := loadGitSigning(ctx)
	if ok {
		c.Sign = g.signer(env)
	}
	return g, ok
}

// appAccessHint explains a repository a GitHub App sign-in cannot see: the
// app reaches only the repositories it is installed on. Other sign-ins get
// no hint.
func appAccessHint(tok, repo string) string {
	if !github.IsAppToken(tok) {
		return ""
	}
	return fmt.Sprintf("\n  You signed in through the dothaven GitHub App, which reaches only the repositories\n  you installed it on. Give it access to %s at %s", repo, appInstallURL())
}

func appInstallURL() string {
	if slug := github.AppSlugFromEnv(); slug != "" {
		return "https://github.com/apps/" + slug + "/installations/new"
	}
	return "https://github.com/settings/installations"
}

func createRepoErr(tok, repo string, err error) error {
	var apiErr *github.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case 422:
			return fmt.Errorf("%s already exists, but this sign-in cannot see it%s", repo, appAccessHint(tok, repo))
		case 403:
			_, name, _ := strings.Cut(repo, "/")
			return fmt.Errorf("this sign-in may not create repositories. Create a private one named %s at https://github.com/new, then push again%s", name, appAccessHint(tok, repo))
		}
	}
	return fmt.Errorf("could not create %s: %w", repo, err)
}

// largeReadable is how many files a readable push holds before dothaven asks.
// Readable pushes are for browsing config on GitHub; tens of thousands of
// files (a plugin folder, say) take long to upload and bury the config.
const largeReadable = 3000

// confirmLargeReadable asks before a readable push of more than
// largeReadable files, naming the folders that make it large.
func confirmLargeReadable(files []github.File, yes bool) bool {
	if len(files) <= largeReadable || yes {
		return true
	}
	counts := map[string]int{}
	for _, f := range files {
		parts := strings.SplitN(f.Path, "/", 4)
		if len(parts) > 3 {
			parts = parts[:3]
		}
		counts[strings.Join(parts, "/")]++
	}
	type folder struct {
		name string
		n    int
	}
	var top []folder
	for k, n := range counts {
		top = append(top, folder{k, n})
	}
	sort.Slice(top, func(i, j int) bool { return top[i].n > top[j].n })
	fmt.Printf("\n%s This readable push has %s. Most of them are in:\n", warn("⚠"), plural(len(files), "file"))
	for _, f := range top[:min(3, len(top))] {
		fmt.Printf("  %-44s %s\n", f.name, plural(f.n, "file"))
	}
	fmt.Println(dim("  An encrypted push uploads one file instead (--mode encrypted), or leave a category out with --skip."))
	if !tui.Interactive() {
		fmt.Println(dim("  Continuing, as this is not a terminal. Pass --yes to skip this note."))
		return true
	}
	ok, err := tui.Confirm(fmt.Sprintf("Upload %s as readable files anyway?", plural(len(files), "file")))
	return err == nil && ok
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
	c.Flags().StringVar(&mode, "mode", "", "encrypted (default), split, or plain — explained in dothaven github --help")
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

	signing, _ := signCommits(ctx, env, c, me)

	r, err := c.GetRepo(ctx, repo)
	switch {
	case errors.Is(err, github.ErrNotFound):
		owner, name, _ := strings.Cut(repo, "/")
		if owner != me.Login {
			return fmt.Errorf("%s does not exist, or this sign-in cannot see it (dothaven only creates repositories on your own account)%s", repo, appAccessHint(c.Token, repo))
		}
		if err := confirmWrite(os.Stderr, fmt.Sprintf("Create the private repository %s?", repo), o.yes); err != nil {
			return err
		}
		if r, err = c.CreatePrivateRepo(ctx, name, "Private backup of my dev setup, made by dothaven — keep this private."); err != nil {
			return createRepoErr(c.Token, repo, err)
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

	tmp, done, err := sys.PrivateTempDir("push")
	if err != nil {
		return err
	}
	defer done()

	digest := &backup.DigestSink{}
	// Fonts are binaries, often hundreds of megabytes, and GitHub takes at most
	// 100 MB per file: pushed only when asked for by name. File backups carry them.
	skip := o.skip
	if !contains(o.only, "fonts") && !contains(skip, "fonts") {
		skip = append(append([]string(nil), skip...), "fonts")
		if dirHasFiles(filepath.Join(env.Home(), fontsDir())) {
			fmt.Println(dim("Your fonts stay out of GitHub pushes (large binaries) — `dothaven backup --encrypt` carries them, or add --only fonts,…"))
		}
	}
	runlog.stepf("push %s as %s, mode %s", r.FullName, machine, mode)
	files, out, err := buildPushFiles(ctx, cmd, env, tmp, mode, pass, o.only, skip, digest)
	if err != nil {
		if ctx.Err() != nil {
			fmt.Fprintln(os.Stderr, "Push cancelled. Nothing was uploaded.")
			return ExitError{Code: 130}
		}
		return err
	}
	if mode != modeEncrypted && !confirmLargeReadable(files, o.yes) {
		return ExitError{Code: 1}
	}
	// Encrypted means encrypted: check the bytes about to leave this machine,
	// not the flag that asked for them.
	var header []byte
	for _, f := range files {
		// Only what this push encrypted: a user's own .age file in the tree
		// (chezmoi's armored key.txt.age) is theirs, not ours to vouch for.
		if (f.Path != "backup.tar.gz.age" && f.Path != "secrets.tar.gz.age") || filepath.Dir(f.Src) != tmp {
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
			printLeftOut(out.res, mode != modePlain, "dothaven github push --mode encrypted")
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
	fmt.Printf("\nUploading %s (%s) to %s\n", plural(len(files), "file"), humanBytes(total), r.FullName)
	runlog.stepf("uploading %d files (%s)", len(files), humanBytes(total))
	var sent int64
	c.Sent = &sent
	stop := startActivity("Uploading", &sent, len(files), nil)
	sha, err := c.Commit(ctx, r.FullName, firstNonEmpty(r.DefaultBranch, cfg.Branch), "machines/"+machine, files,
		map[string]string{"README.md": repoReadme(r.FullName)},
		fmt.Sprintf("dothaven: %s, %s backup, %s", machine, mode, plural(out.res.TotalFiles, "file")))
	stop()
	if err != nil {
		if ctx.Err() != nil {
			fmt.Fprintln(os.Stderr, "Push cancelled. Nothing was committed; the last push on GitHub is unchanged.")
			return ExitError{Code: 130}
		}
		if errors.Is(err, github.ErrNotFound) {
			return fmt.Errorf("GitHub would not let this sign-in write to %s%s", r.FullName, appAccessHint(c.Token, r.FullName))
		}
		return err
	}
	cfg.Repo, cfg.Mode = r.FullName, mode
	if err := saveGitHubConfig(env, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "  %s could not save your repo choice: %v\n", warn("⚠"), err)
	}
	if sha == "" {
		fmt.Printf("%s Already up to date — nothing changed since the last push.\n", good("✓"))
		printLeftOut(out.res, mode != modePlain, "dothaven github push --mode encrypted")
		return nil
	}
	verified := ""
	if c.Signing.Verified {
		verified = ", Verified"
	}
	fmt.Printf("%s Pushed %s as machines/%s %s\n", good("✓"), plural(out.res.TotalFiles, "file"), machine, dim("("+mode+", commit "+sha[:7]+verified+")"))
	fmt.Printf("  %s\n", r.HTMLURL+"/tree/"+firstNonEmpty(r.DefaultBranch, "main")+"/machines/"+machine)
	if note := signingNote(c.Signing, signing); note != "" {
		fmt.Printf("  %s %s\n", warn("⚠"), note)
	}
	printLeftOut(out.res, mode != modePlain, "dothaven github push --mode encrypted")
	fmt.Println("\n" + bold("On the new machine:"))
	fmt.Printf("  %s\n  %s\n", kbd("dothaven github login"), kbd("dothaven restore github"))
	if mode != modePlain {
		fmt.Println(dim("  You will need the passphrase. Nothing can open the encrypted part without it."))
	}
	return nil
}

// fontsDir is the user font folder, relative to home.
func fontsDir() string {
	if runtime.GOOS == "darwin" {
		return filepath.Join("Library", "Fonts")
	}
	return filepath.Join(".local", "share", "fonts")
}

func dirHasFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
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
	p, exact, err := st.GetExact(accountPassphrase)
	if err == nil && !exact {
		// Saved by an older version, in a form that may not read back as
		// typed. A wrong remembered passphrase would silently encrypt the
		// copy on GitHub with something you don't know; ask once instead.
		fmt.Println(dim("Your remembered passphrase was saved by an older dothaven — type it once more so it is stored exactly."))
	}
	if err == nil && exact && len([]rune(p)) >= minPassphrase {
		fmt.Println(dim("Using your remembered backup passphrase (forget it with `dothaven github logout`)."))
		return p, nil
	}
	p, err = newPassphrase()
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
		// A .git folder, or the .git file a submodule or worktree has in its
		// place: GitHub refuses any path component named .git.
		if strings.EqualFold(d.Name(), ".git") {
			skipped++
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
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
		fmt.Printf("  %s %s left out of the readable copy (git cannot store a .git inside a repository; --mode encrypted keeps them)\n", dim("•"), plural(skipped, "entry")+" named .git")
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
	c.Flags().StringSliceVar(&o.only, "only", nil, "only these categories (comma-separated)")
	c.Flags().StringSliceVar(&o.skip, "skip", nil, "skip these categories (comma-separated)")
	c.Flags().BoolVar(&o.keepPaths, "keep-paths", false, "don't rewrite the old machine's home folder path to this one's")
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
func openGitHubBackup(ctx context.Context, env *sys.OS, spec string, dirs ...string) (string, func(), error) {
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
			return "", noop, fmt.Errorf("no repository %s (or this login cannot see it) — push from the old machine first: dothaven github push%s", repo, appAccessHint(c.Token, repo))
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

	tmp, cleanup, err := sys.PrivateTempDir("github")
	if err != nil {
		return "", noop, err
	}
	fail := func(err error) (string, func(), error) { cleanup(); return "", noop, err }

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
		dir, inner, err := openBackupOnly(ctx, env, archive, dirs...)
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
		sdir, inner, err := openBackupOnly(ctx, env, secretsPath, dirs...)
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
		// Merged: the bundle itself is not a file to restore anywhere.
		_ = os.Remove(secretsPath)
	}
	return stable, cleanup, nil
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}
