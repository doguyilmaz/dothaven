package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"unicode/utf8"
)

// File is one file to commit, read from Src on disk.
type File struct {
	Path string // slash-separated, relative to the commit's prefix
	Src  string
	Exec bool
	Size int64
}

// MaxFile is the largest file the API accepts in one blob, with headroom.
const MaxFile = 95 << 20

// inlineMax is the largest text file sent inline in a tree request instead of
// as its own blob — one request instead of hundreds for a folder of configs.
const inlineMax = 256 << 10

// inlineBatch bounds how much inline content one tree request carries.
const inlineBatch = 4 << 20

// uploadWorkers bounds concurrent blob uploads: enough to hide latency, few
// enough not to trip GitHub's secondary rate limits.
const uploadWorkers = 4

type treeEntry struct {
	Path    string  `json:"path"`
	Mode    string  `json:"mode"`
	Type    string  `json:"type"`
	SHA     *string `json:"sha,omitempty"`
	Content *string `json:"content,omitempty"`
}

// Commit replaces everything under prefix on branch with files (plus extra,
// small in-memory files such as a README at the repository root) as one
// commit, and returns its SHA — or "" when nothing changed.
//
// The subtree under prefix is rebuilt from scratch, so a file deleted on this
// machine disappears from the repository too; the rest of the repository
// (other machines) is untouched. The branch only moves fast-forward: if
// another push landed in between, the commit is rebuilt on top of it once.
func (c *Client) Commit(ctx context.Context, repo, branch, prefix string, files []File, root map[string]string, message string) (string, error) {
	for _, f := range files {
		if f.Size > MaxFile {
			return "", fmt.Errorf("%s is %d MB; GitHub takes at most 100 MB per file", f.Path, f.Size>>20)
		}
	}
	sub, err := c.buildTree(ctx, repo, files)
	if err != nil {
		return "", err
	}
	for attempt := 0; ; attempt++ {
		sha, err := c.commitOnce(ctx, repo, branch, prefix, sub, root, message)
		var apiErr *APIError
		if attempt == 0 && errors.As(err, &apiErr) && apiErr.Status == http.StatusUnprocessableEntity {
			continue // not a fast-forward: someone pushed meanwhile; rebase once
		}
		return sha, err
	}
}

func (c *Client) commitOnce(ctx context.Context, repo, branch, prefix, sub string, root map[string]string, message string) (string, error) {
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	parent, baseTree := "", ""
	err := c.do(ctx, http.MethodGet, "/repos/"+repo+"/git/ref/heads/"+branch, nil, "", &ref)
	switch {
	case err == nil:
		parent = ref.Object.SHA
		var commit struct {
			Tree struct {
				SHA string `json:"sha"`
			} `json:"tree"`
		}
		if err := c.do(ctx, http.MethodGet, "/repos/"+repo+"/git/commits/"+parent, nil, "", &commit); err != nil {
			return "", err
		}
		baseTree = commit.Tree.SHA
	case errors.Is(err, ErrNotFound) || isEmptyRepo(err):
		// An empty repository (made by hand, without a README) has no branch
		// yet; the first commit creates it.
	default:
		return "", err
	}

	entries := []treeEntry{{Path: prefix, Mode: "040000", Type: "tree", SHA: &sub}}
	for p, content := range root {
		entries = append(entries, treeEntry{Path: p, Mode: "100644", Type: "blob", Content: &content})
	}
	rootTree, err := c.createTree(ctx, repo, baseTree, entries)
	if err != nil {
		return "", err
	}
	if rootTree == baseTree {
		return "", nil // byte-for-byte what is already there
	}

	cb := map[string]any{"message": message, "tree": rootTree}
	if parent != "" {
		cb["parents"] = []string{parent}
	}
	b, err := jsonBody(cb)
	if err != nil {
		return "", err
	}
	var commit struct {
		SHA string `json:"sha"`
	}
	if err := c.do(ctx, http.MethodPost, "/repos/"+repo+"/git/commits", b, "", &commit); err != nil {
		return "", err
	}

	if parent == "" {
		b, _ = jsonBody(map[string]string{"ref": "refs/heads/" + branch, "sha": commit.SHA})
		return commit.SHA, c.do(ctx, http.MethodPost, "/repos/"+repo+"/git/refs", b, "", nil)
	}
	b, _ = jsonBody(map[string]any{"sha": commit.SHA, "force": false})
	return commit.SHA, c.do(ctx, http.MethodPatch, "/repos/"+repo+"/git/refs/heads/"+branch, b, "", nil)
}

func isEmptyRepo(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusConflict
}

// buildTree uploads files and returns the SHA of a tree holding exactly them.
// Small text files ride inline in the tree requests; everything else is
// uploaded as a blob, a few at a time, streamed from disk so a large archive
// is never held in memory twice.
func (c *Client) buildTree(ctx context.Context, repo string, files []File) (string, error) {
	var inline, blobs []File
	for _, f := range files {
		if f.Size <= inlineMax && isText(f.Src) {
			inline = append(inline, f)
		} else {
			blobs = append(blobs, f)
		}
	}

	shas := make([]string, len(blobs))
	errs := make([]error, len(blobs))
	work := make(chan int)
	var wg sync.WaitGroup
	for range min(uploadWorkers, max(1, len(blobs))) {
		wg.Go(func() {
			for i := range work {
				shas[i], errs[i] = c.uploadBlob(ctx, repo, blobs[i].Src, blobs[i].Size)
			}
		})
	}
	for i := range blobs {
		if ctx.Err() != nil {
			break
		}
		work <- i
	}
	close(work)
	wg.Wait()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	var entries []treeEntry
	for i, f := range blobs {
		if errs[i] != nil {
			return "", fmt.Errorf("uploading %s: %w", f.Path, errs[i])
		}
		entries = append(entries, treeEntry{Path: f.Path, Mode: mode(f), Type: "blob", SHA: &shas[i]})
	}

	// Inline files in batches; each tree builds on the previous one.
	tree := ""
	batch := entries
	size := 0
	flush := func() error {
		if len(batch) == 0 && tree != "" {
			return nil
		}
		t, err := c.createTree(ctx, repo, tree, batch)
		if err != nil {
			return err
		}
		tree, batch, size = t, nil, 0
		return nil
	}
	for _, f := range inline {
		b, err := os.ReadFile(f.Src)
		if err != nil {
			return "", err
		}
		content := string(b)
		if size+len(content) > inlineBatch && len(batch) > 0 {
			if err := flush(); err != nil {
				return "", err
			}
		}
		batch = append(batch, treeEntry{Path: f.Path, Mode: mode(f), Type: "blob", Content: &content})
		size += len(content)
	}
	if err := flush(); err != nil {
		return "", err
	}
	return tree, nil
}

func mode(f File) string {
	if f.Exec {
		return "100755"
	}
	return "100644"
}

// isText reports whether a file can travel as a JSON string: valid UTF-8 and
// no NUL bytes.
func isText(path string) bool {
	b, err := os.ReadFile(path)
	return err == nil && utf8.Valid(b) && bytes.IndexByte(b, 0) < 0
}

func (c *Client) createTree(ctx context.Context, repo, base string, entries []treeEntry) (string, error) {
	req := map[string]any{"tree": entries}
	if base != "" {
		req["base_tree"] = base
	}
	b, err := jsonBody(req)
	if err != nil {
		return "", err
	}
	var out struct {
		SHA string `json:"sha"`
	}
	err = c.do(ctx, http.MethodPost, "/repos/"+repo+"/git/trees", b, "", &out)
	return out.SHA, err
}

// uploadBlob streams a file as a base64 blob. The body is generated on the
// fly (and again on a retry), with an exact Content-Length.
func (c *Client) uploadBlob(ctx context.Context, repo, src string, size int64) (string, error) {
	const pre, post = `{"encoding":"base64","content":"`, `"}`
	b := func() (io.Reader, int64, error) {
		f, err := os.Open(src)
		if err != nil {
			return nil, 0, err
		}
		pr, pw := io.Pipe()
		go func() {
			defer f.Close()
			enc := base64.NewEncoder(base64.StdEncoding, pw)
			_, err := io.Copy(enc, f)
			if err == nil {
				err = enc.Close()
			}
			pw.CloseWithError(err)
		}()
		n := int64(len(pre)) + int64(base64.StdEncoding.EncodedLen(int(size))) + int64(len(post))
		return io.MultiReader(strings.NewReader(pre), pr, strings.NewReader(post)), n, nil
	}
	var out struct {
		SHA string `json:"sha"`
	}
	err := c.do(ctx, http.MethodPost, "/repos/"+repo+"/git/blobs", b, "", &out)
	return out.SHA, err
}

// Tarball streams the repository at ref as a .tar.gz into w.
func (c *Client) Tarball(ctx context.Context, repo, ref string, w io.Writer) error {
	return c.do(ctx, http.MethodGet, "/repos/"+repo+"/tarball/"+ref, nil, "", w)
}

// Dir lists a directory in the repository.
func (c *Client) Dir(ctx context.Context, repo, path string) ([]string, error) {
	var items []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := c.do(ctx, http.MethodGet, "/repos/"+repo+"/contents/"+path, nil, "", &items); err != nil {
		return nil, err
	}
	var out []string
	for _, it := range items {
		if it.Type == "dir" {
			out = append(out, it.Name)
		}
	}
	return out, nil
}

// Raw reads one file's content (up to 1 MiB).
func (c *Client) Raw(ctx context.Context, repo, path string) ([]byte, error) {
	var buf bytes.Buffer
	err := c.do(ctx, http.MethodGet, "/repos/"+repo+"/contents/"+path, nil, "application/vnd.github.raw", &limited{w: &buf, n: 1 << 20})
	return buf.Bytes(), err
}

type limited struct {
	w io.Writer
	n int64
}

func (l *limited) Write(p []byte) (int, error) {
	if int64(len(p)) > l.n {
		return 0, errors.New("file too large")
	}
	l.n -= int64(len(p))
	return l.w.Write(p)
}
