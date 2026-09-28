package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/doguyilmaz/dothaven/internal/github"
	"github.com/doguyilmaz/dothaven/internal/github/githubtest"
	"github.com/doguyilmaz/dothaven/internal/sys"
)

// GitHub refuses a path component named .git — a folder, or the .git file a
// submodule or worktree has instead — so a readable push leaves both out.
func TestTreeFilesLeavesOutGitEntries(t *testing.T) {
	root := t.TempDir()
	for p, body := range map[string]string{
		"editor/nvim/init.lua":                       "vim.o.number = true\n",
		"editor/nvim/pack/p/start/plugin/.git":       "gitdir: ../../../../.git/modules/plugin\n",
		"editor/nvim/pack/p/start/plugin/plugin.lua": "return {}\n",
		"extra/.dotfiles/.git/config":                "[core]\n",
		"extra/.dotfiles/zshrc":                      "alias ll=ls\n",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, p), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	files, err := treeFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range files {
		got = append(got, f.Path)
		for _, part := range strings.Split(f.Path, "/") {
			if part == ".git" {
				t.Errorf("%s would be refused by GitHub", f.Path)
			}
		}
	}
	if len(got) != 3 {
		t.Errorf("files = %v, want the three that are not .git", got)
	}
}

// signInEnv points dothaven at a fake GitHub App with an on-disk credential
// store in a fresh home.
func signInEnv(t *testing.T) (*sys.OS, *githubtest.Server) {
	t.Helper()
	s := githubtest.New("ghu_current", "dev")
	t.Cleanup(s.Close)
	s.Refresh = "ghr_current"
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("DOTHAVEN_SECRET_STORE", "file")
	t.Setenv("DOTHAVEN_GITHUB_API", s.URL)
	t.Setenv("DOTHAVEN_GITHUB_WEB", s.URL)
	t.Setenv("DOTHAVEN_GITHUB_CLIENT_ID", "client-id")
	t.Setenv(tokenEnv, "")
	t.Setenv("PATH", "") // no gh login to fall back on
	return sys.Real(), s
}

func TestResolveTokenRenewsAStaleAppSignIn(t *testing.T) {
	env, s := signInEnv(t)
	ctx := context.Background()
	st := secrets(env)
	stale := github.Token{Access: "ghu_current", Refresh: "ghr_current", Expires: time.Now().Add(-time.Minute)}
	if err := st.Set(accountToken, encodeToken(stale)); err != nil {
		t.Fatal(err)
	}

	// Read-only callers report it and leave the pair alone: a refresh token
	// works once, and a renewal that is not kept loses the sign-in.
	tok, _, err := resolveToken(ctx, env, false)
	if !errors.Is(err, errRenewDue) || tok != "ghu_current" || s.Refreshes != 0 {
		t.Fatalf("read-only: tok %q, err %v, refreshes %d", tok, err, s.Refreshes)
	}

	tok, _, err = resolveToken(ctx, env, true)
	if err != nil || tok != s.Token || tok == "ghu_current" || s.Refreshes != 1 {
		t.Fatalf("renew: tok %q (server %q), err %v, refreshes %d", tok, s.Token, err, s.Refreshes)
	}
	raw, _ := st.Get(accountToken)
	kept := decodeToken(raw)
	if kept.Access != s.Token || kept.Refresh != s.Refresh || kept.Stale(time.Now()) {
		t.Fatalf("the renewed pair was not kept: %+v", kept)
	}

	// Fresh now: no second renewal.
	if tok2, _, err := resolveToken(ctx, env, true); err != nil || tok2 != tok || s.Refreshes != 1 {
		t.Errorf("fresh token renewed again: %q %v %d", tok2, err, s.Refreshes)
	}
}

// Two runs renewing at once: the second's refresh token is already spent, and
// it takes the pair the first one saved instead of signing the user out.
func TestRenewTokenAfterAnotherRunRenewed(t *testing.T) {
	env, _ := signInEnv(t)
	st := secrets(env)
	theirs := github.Token{Access: "ghu_theirs", Refresh: "ghr_theirs", Expires: time.Now().Add(8 * time.Hour)}
	if err := st.Set(accountToken, encodeToken(theirs)); err != nil {
		t.Fatal(err)
	}
	mine := github.Token{Access: "ghu_old", Refresh: "ghr_spent", Expires: time.Now().Add(-time.Minute)}
	got, err := renewToken(context.Background(), st, mine)
	if err != nil || got.Access != "ghu_theirs" {
		t.Fatalf("got %+v, %v", got, err)
	}

	// Nobody renewed: the sign-in really is over.
	if err := st.Set(accountToken, encodeToken(mine)); err != nil {
		t.Fatal(err)
	}
	if _, err := renewToken(context.Background(), st, mine); !errors.Is(err, github.ErrSignInExpired) {
		t.Errorf("spent refresh token: %v, want ErrSignInExpired", err)
	}
}

func TestStoredTokenFormats(t *testing.T) {
	// A bare token (--with-token, gh, older versions) never goes stale.
	if tok := decodeToken("github_pat_abc"); tok.Access != "github_pat_abc" || tok.Stale(time.Now().AddDate(5, 0, 0)) {
		t.Errorf("bare token: %+v", tok)
	}
	if s := encodeToken(github.Token{Access: "ghp_x"}); s != "ghp_x" {
		t.Errorf("a token without refresh is stored bare, got %q", s)
	}
	pair := github.Token{Access: "ghu_a", Refresh: "ghr_b", Expires: time.Now().Add(time.Hour).Round(time.Second)}
	if got := decodeToken(encodeToken(pair)); got.Access != pair.Access || got.Refresh != pair.Refresh || !got.Expires.Equal(pair.Expires) {
		t.Errorf("round trip: %+v -> %+v", pair, got)
	}
}

func TestAppAccessHints(t *testing.T) {
	t.Setenv("DOTHAVEN_GITHUB_APP", "dothaven")
	if h := appAccessHint("ghp_classic", "me/dothaven-backup"); h != "" {
		t.Errorf("a non-app token gets no app hint: %q", h)
	}
	h := appAccessHint("ghu_x", "me/dothaven-backup")
	if !strings.Contains(h, "me/dothaven-backup") || !strings.Contains(h, "https://github.com/apps/dothaven/installations/new") {
		t.Errorf("hint = %q", h)
	}

	exists := createRepoErr("ghu_x", "me/dothaven-backup", &github.APIError{Status: 422, Message: "name already exists"})
	if !strings.Contains(exists.Error(), "already exists, but this sign-in cannot see it") || !strings.Contains(exists.Error(), "installations/new") {
		t.Errorf("422: %v", exists)
	}
	forbidden := createRepoErr("ghu_x", "me/dothaven-backup", &github.APIError{Status: 403, Message: "Resource not accessible by integration"})
	if !strings.Contains(forbidden.Error(), "https://github.com/new") || !strings.Contains(forbidden.Error(), "named dothaven-backup") {
		t.Errorf("403: %v", forbidden)
	}
}
