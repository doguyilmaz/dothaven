package github

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/doguyilmaz/dothaven/internal/github/githubtest"
)

func client(t *testing.T, s *githubtest.Server) *Client {
	t.Helper()
	t.Setenv("DOTHAVEN_GITHUB_API", s.URL)
	t.Setenv("DOTHAVEN_GITHUB_WEB", s.URL)
	c, err := New(s.Token)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestDeviceFlow(t *testing.T) {
	s := githubtest.New("tok", "dev")
	defer s.Close()
	s.DeviceApproveAfter = 2 // two "pending" answers before approval
	minPollInterval = 10 * time.Millisecond
	c := client(t, s)
	c.Token = ""
	dc, err := c.StartDeviceFlow(context.Background(), "client-id")
	if err != nil || dc.UserCode == "" {
		t.Fatalf("start: %+v %v", dc, err)
	}
	dc.Interval = 0
	tok, err := c.WaitForToken(context.Background(), "client-id", dc)
	if err != nil || tok.Access != "tok" {
		t.Fatalf("token = %+v, %v", tok, err)
	}
	if tok.Refresh != "" || !tok.Expires.IsZero() || tok.Stale(time.Now().AddDate(10, 0, 0)) {
		t.Errorf("an OAuth app token never expires: %+v", tok)
	}
}

// A GitHub App sign-in lasts 8 hours; its refresh token buys a new pair once.
func TestAppSignInRenews(t *testing.T) {
	s := githubtest.New("ghu_first", "dev")
	defer s.Close()
	s.Refresh = "ghr_first"
	minPollInterval = 10 * time.Millisecond
	c := client(t, s)
	c.Token = ""
	ctx := context.Background()
	dc, err := c.StartDeviceFlow(ctx, "client-id")
	if err != nil {
		t.Fatal(err)
	}
	dc.Interval = 0
	tok, err := c.WaitForToken(ctx, "client-id", dc)
	if err != nil {
		t.Fatal(err)
	}
	if tok.Access != "ghu_first" || tok.Refresh != "ghr_first" {
		t.Fatalf("token = %+v", tok)
	}
	if d := time.Until(tok.Expires); d < 7*time.Hour+59*time.Minute || d > 8*time.Hour {
		t.Errorf("expires in %v, want 8h", d)
	}
	if d := time.Until(tok.RefreshExpires); d < 183*24*time.Hour || d > 184*24*time.Hour {
		t.Errorf("refresh token expires in %v, want about 6 months", d)
	}
	if tok.Stale(time.Now()) || !tok.Stale(tok.Expires.Add(-4*time.Minute)) {
		t.Error("a token is stale from five minutes before its expiry, not before")
	}

	fresh, err := c.RefreshToken(ctx, "client-id", tok.Refresh)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Access != s.Token || fresh.Access == tok.Access || fresh.Refresh == tok.Refresh || fresh.Stale(time.Now()) {
		t.Fatalf("renewed = %+v", fresh)
	}
	if _, err := c.RefreshToken(ctx, "client-id", tok.Refresh); !errors.Is(err, ErrSignInExpired) {
		t.Errorf("a spent refresh token: %v, want ErrSignInExpired", err)
	}
}

func TestAppSlugFromEnv(t *testing.T) {
	for in, want := range map[string]string{
		"dothaven": "dothaven", "dot-haven2": "dot-haven2",
		"": "", "DotHaven": "", "../users/x": "", "a/b": "", "-x": "", "x[bot]": "",
	} {
		t.Setenv("DOTHAVEN_GITHUB_APP", in)
		if got := AppSlugFromEnv(); got != want {
			t.Errorf("AppSlugFromEnv(%q) = %q, want %q", in, got, want)
		}
	}
}

// Commits name the account as author and the app's bot as committer — the
// first one too, which an empty repository gets from the Contents API.
func TestCommitsSignedByBot(t *testing.T) {
	s := githubtest.New("tok", "dev")
	defer s.Close()
	s.AppSlug = "dothaven"
	s.AddEmptyRepo("dev/dothaven-backup", true)
	c := client(t, s)
	ctx := context.Background()

	bot, err := c.Bot(ctx, "dothaven")
	if err != nil {
		t.Fatal(err)
	}
	if bot.Name != "dothaven[bot]" || bot.Email != "2002+dothaven[bot]@users.noreply.github.com" {
		t.Fatalf("bot = %+v", bot)
	}
	if _, err := c.Bot(ctx, "no-such-app"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown app: %v", err)
	}
	me, err := c.Me(ctx)
	if err != nil || me.ID != githubtest.UserID {
		t.Fatalf("me = %+v, %v", me, err)
	}
	author := NoReply(me.Login, me.ID)
	c.Author, c.Committer = &author, &bot

	dir := t.TempDir()
	files := []File{write(t, dir, "shell/.zshrc", "alias ll='ls -la'\n", 0o644)}
	if _, err := c.Commit(ctx, "dev/dothaven-backup", "main", "machines/m", files, map[string]string{"README.md": "# x\n"}, "push"); err != nil {
		t.Fatal(err)
	}
	want := githubtest.Signature{
		Author:    "dev <1001+dev@users.noreply.github.com>",
		Committer: "dothaven[bot] <2002+dothaven[bot]@users.noreply.github.com>",
	}
	sigs := s.Signatures()
	if len(sigs) != 2 {
		t.Fatalf("want the seed commit and the push, got %+v", sigs)
	}
	for i, sig := range sigs {
		if sig != want {
			t.Errorf("commit %d signed %+v, want %+v", i, sig, want)
		}
	}
}

func TestRefusesPlainHTTPElsewhere(t *testing.T) {
	t.Setenv("DOTHAVEN_GITHUB_API", "http://evil.example.com")
	if _, err := New("t"); err == nil {
		t.Error("a non-loopback http API URL must be refused — the token would travel in the clear")
	}
}

func write(t *testing.T, dir, rel, body string, mode os.FileMode) File {
	t.Helper()
	p := filepath.Join(dir, rel)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	return File{Path: rel, Src: p, Size: int64(len(body)), Exec: mode&0o100 != 0}
}

func TestCommitRoundTrip(t *testing.T) {
	s := githubtest.New("tok", "dev")
	defer s.Close()
	c := client(t, s)
	ctx := context.Background()

	repo, err := c.CreatePrivateRepo(ctx, "dothaven-backup", "x")
	if err != nil || !repo.Private || repo.FullName != "dev/dothaven-backup" {
		t.Fatalf("create: %+v %v", repo, err)
	}
	s.AddRepo("dev/other-machine-stays", true)

	dir := t.TempDir()
	files := []File{
		write(t, dir, "shell/.zshrc", "alias ll='ls -la'\n", 0o644),
		write(t, dir, "hooks/pre-commit", "#!/bin/sh\n", 0o755),
		// binary → uploaded as a streamed blob, not inline
		write(t, dir, "backup.tar.gz.age", "age\x00\x01\x02binary", 0o600),
	}
	sha, err := c.Commit(ctx, repo.FullName, "main", "machines/box", files, map[string]string{"README.md": "hi\n"}, "backup")
	if err != nil || sha == "" {
		t.Fatalf("commit: %q %v", sha, err)
	}
	if b, ok := s.File(repo.FullName, "main", "machines/box/backup.tar.gz.age"); !ok || !bytes.Equal(b, []byte("age\x00\x01\x02binary")) {
		t.Errorf("binary blob did not survive: %q", b)
	}

	// Same content again: nothing to commit, nothing pushed.
	pushes := s.Pushes
	sha, err = c.Commit(ctx, repo.FullName, "main", "machines/box", files, map[string]string{"README.md": "hi\n"}, "backup")
	if err != nil || sha != "" || s.Pushes != pushes {
		t.Errorf("unchanged push made a commit: %q %v", sha, err)
	}

	// A file removed locally disappears remotely; another machine is untouched.
	if _, err := c.Commit(ctx, repo.FullName, "main", "machines/laptop", files[:1], nil, "laptop"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Commit(ctx, repo.FullName, "main", "machines/box", files[:1], nil, "smaller"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.File(repo.FullName, "main", "machines/box/hooks/pre-commit"); ok {
		t.Error("a file deleted locally is still in the repo")
	}
	if _, ok := s.File(repo.FullName, "main", "machines/laptop/shell/.zshrc"); !ok {
		t.Error("pushing one machine removed another's files")
	}

	machines, err := c.Dir(ctx, repo.FullName, "machines")
	if err != nil || strings.Join(machines, ",") != "box,laptop" {
		t.Errorf("machines = %v %v", machines, err)
	}

	var buf bytes.Buffer
	if err := c.Tarball(ctx, repo.FullName, "main", &buf); err != nil {
		t.Fatal(err)
	}
	gz, err := gzip.NewReader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	found := false
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if strings.HasSuffix(h.Name, "machines/laptop/shell/.zshrc") {
			found = true
		}
	}
	if !found {
		t.Error("tarball lacks the committed file")
	}
}

func TestNotFoundAndBadToken(t *testing.T) {
	s := githubtest.New("tok", "dev")
	defer s.Close()
	c := client(t, s)
	if _, err := c.GetRepo(context.Background(), "dev/missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing repo err = %v", err)
	}
	c.Token = "wrong"
	var apiErr *APIError
	if _, err := c.Me(context.Background()); !errors.As(err, &apiErr) || apiErr.Status != 401 {
		t.Errorf("bad token err = %v", err)
	}
}

// A repository made by hand without a README has no commits, and GitHub's
// Git Data API refuses blobs and trees in it; the first push seeds it through
// the Contents API and then commits as usual.
func TestCommitIntoEmptyRepository(t *testing.T) {
	s := githubtest.New("tok", "dev")
	defer s.Close()
	c := client(t, s)
	ctx := context.Background()
	s.AddEmptyRepo("dev/by-hand", true)

	dir := t.TempDir()
	files := []File{write(t, dir, "shell/.zshrc", "alias ll='ls -la'\n", 0o644)}
	sha, err := c.Commit(ctx, "dev/by-hand", "main", "machines/box", files, map[string]string{"README.md": "hi\n"}, "backup")
	if err != nil || sha == "" {
		t.Fatalf("commit into an empty repository: %q %v", sha, err)
	}
	if b, ok := s.File("dev/by-hand", "main", "machines/box/shell/.zshrc"); !ok || string(b) != "alias ll='ls -la'\n" {
		t.Errorf("file not committed: %q", b)
	}
	if b, ok := s.File("dev/by-hand", "main", "README.md"); !ok || string(b) != "hi\n" {
		t.Errorf("README = %q", b)
	}
}
