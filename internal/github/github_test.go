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
	if err != nil || tok != "tok" {
		t.Fatalf("token = %q, %v", tok, err)
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
