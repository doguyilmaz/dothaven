// Package github is the small slice of the GitHub API dothaven needs to keep a
// private backup repository: sign in with the device flow, create the repo,
// write a commit through the Git Data API, and read it back as a tarball.
//
// It talks HTTPS directly rather than shelling out to git: the machine being
// restored is often a fresh one with no git (on macOS, git arrives with the
// Xcode command-line tools), and the token never has to be put anywhere git
// could store it.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// ClientID is the OAuth app used for browser sign-in (the device flow). It is
// public by design — the device flow has no client secret — and is set at
// release time with -ldflags, or with DOTHAVEN_GITHUB_CLIENT_ID.
var ClientID = ""

// ClientIDFromEnv returns the OAuth client ID in effect.
func ClientIDFromEnv() string {
	if v := os.Getenv("DOTHAVEN_GITHUB_CLIENT_ID"); v != "" {
		return v
	}
	return ClientID
}

// Client is an authenticated GitHub API client.
type Client struct {
	API   string // https://api.github.com
	Web   string // https://github.com
	Token string
	HTTP  *http.Client
}

// ErrNotFound is a 404: missing, or not visible to this token (GitHub does
// not say which for private repositories).
var ErrNotFound = errors.New("not found")

// APIError is any other failed call.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return fmt.Sprintf("GitHub API %d: %s", e.Status, e.Message) }

// New returns a client for github.com. DOTHAVEN_GITHUB_API / _WEB point it
// elsewhere — for tests, and for GitHub Enterprise — but only at https, or at
// plain http on the loopback interface.
func New(token string) (*Client, error) {
	c := &Client{API: "https://api.github.com", Web: "https://github.com", Token: token, HTTP: httpClient()}
	for env, dst := range map[string]*string{"DOTHAVEN_GITHUB_API": &c.API, "DOTHAVEN_GITHUB_WEB": &c.Web} {
		if v := os.Getenv(env); v != "" {
			if !safeBase(v) {
				return nil, fmt.Errorf("%s must be an https URL (or http on localhost)", env)
			}
			*dst = strings.TrimRight(v, "/")
		}
	}
	return c, nil
}

func safeBase(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	host := u.Hostname()
	return u.Scheme == "http" && (host == "127.0.0.1" || host == "localhost" || host == "::1")
}

// httpClient bounds every stage of a request that can stall — connecting,
// the TLS handshake, waiting for the first response byte — while leaving the
// body free to take as long as an upload honestly takes. The overall deadline
// is the caller's context.
func httpClient() *http.Client {
	t := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 90 * time.Second,
		IdleConnTimeout:       60 * time.Second,
		MaxIdleConnsPerHost:   8,
		ForceAttemptHTTP2:     true,
	}
	return &http.Client{Transport: t}
}

// body is a request body that can be produced again for a retry.
type body func() (io.Reader, int64, error)

func jsonBody(v any) (body, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return func() (io.Reader, int64, error) { return bytes.NewReader(b), int64(len(b)), nil }, nil
}

const maxRetries = 3

// do sends one API request, retrying on the failures that are worth a retry:
// a 5xx, a rate limit (waiting as long as GitHub asks, up to a minute), or a
// connection dropped before any response.
func (c *Client) do(ctx context.Context, method, path string, b body, accept string, out any) error {
	resp, err := c.send(ctx, method, c.API+path, b, accept)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return nil
	}
	if w, ok := out.(io.Writer); ok {
		_, err := io.Copy(w, resp.Body)
		return err
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 64<<20)).Decode(out)
}

func (c *Client) send(ctx context.Context, method, u string, b body, accept string) (*http.Response, error) {
	var last error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt, last)):
			}
		}
		var r io.Reader
		var n int64 = -1
		if b != nil {
			var err error
			if r, n, err = b(); err != nil {
				return nil, err
			}
		}
		req, err := http.NewRequestWithContext(ctx, method, u, r)
		if err != nil {
			return nil, err
		}
		if n >= 0 {
			req.ContentLength = n
		}
		if accept == "" {
			accept = "application/vnd.github+json"
		}
		req.Header.Set("Accept", accept)
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		req.Header.Set("User-Agent", "dothaven")
		if b != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if c.Token != "" {
			req.Header.Set("Authorization", "Bearer "+c.Token)
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			last = err
			continue
		}
		if resp.StatusCode < 300 {
			return resp, nil
		}
		apiErr := readError(resp)
		resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusNotFound:
			return nil, ErrNotFound
		case resp.StatusCode >= 500, resp.StatusCode == http.StatusTooManyRequests,
			resp.StatusCode == http.StatusForbidden && rateLimited(resp):
			last = &retryAfter{apiErr, resp.Header.Get("Retry-After")}
			continue
		}
		return nil, apiErr
	}
	var ra *retryAfter
	if errors.As(last, &ra) {
		return nil, ra.err
	}
	return nil, last
}

type retryAfter struct {
	err   *APIError
	after string
}

func (r *retryAfter) Error() string { return r.err.Error() }

func backoff(attempt int, last error) time.Duration {
	var ra *retryAfter
	if errors.As(last, &ra) && ra.after != "" {
		if s, err := strconv.Atoi(ra.after); err == nil {
			return min(time.Duration(s)*time.Second, time.Minute)
		}
	}
	return time.Duration(attempt*attempt) * time.Second
}

func rateLimited(resp *http.Response) bool {
	return resp.Header.Get("X-RateLimit-Remaining") == "0" || resp.Header.Get("Retry-After") != ""
}

func readError(resp *http.Response) *APIError {
	var e struct {
		Message string `json:"message"`
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	_ = json.Unmarshal(b, &e)
	if e.Message == "" {
		e.Message = http.StatusText(resp.StatusCode)
	}
	return &APIError{Status: resp.StatusCode, Message: e.Message}
}

// User is the signed-in account.
type User struct {
	Login string `json:"login"`
}

// Me returns who the token belongs to.
func (c *Client) Me(ctx context.Context) (User, error) {
	var u User
	err := c.do(ctx, http.MethodGet, "/user", nil, "", &u)
	return u, err
}

// Repo is the part of a repository dothaven cares about.
type Repo struct {
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"default_branch"`
	HTMLURL       string `json:"html_url"`
}

// GetRepo returns owner/name, or ErrNotFound.
func (c *Client) GetRepo(ctx context.Context, fullName string) (Repo, error) {
	var r Repo
	err := c.do(ctx, http.MethodGet, "/repos/"+fullName, nil, "", &r)
	return r, err
}

// CreatePrivateRepo creates a private repository under the signed-in user,
// with an initial commit so it has a branch to write to.
func (c *Client) CreatePrivateRepo(ctx context.Context, name, description string) (Repo, error) {
	b, err := jsonBody(map[string]any{
		"name": name, "description": description, "private": true, "auto_init": true,
		"has_issues": false, "has_projects": false, "has_wiki": false,
	})
	if err != nil {
		return Repo{}, err
	}
	var r Repo
	err = c.do(ctx, http.MethodPost, "/user/repos", b, "", &r)
	return r, err
}
