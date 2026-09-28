// Package githubtest is an in-memory stand-in for the slice of the GitHub API
// dothaven uses: users, repositories, the Git Data API (blobs, trees, commits,
// refs), contents and tarballs, and the device-flow endpoints. It keeps real
// objects, so a push followed by a pull is a real round trip.
package githubtest

import (
	"archive/tar"
	"cmp"
	"compress/gzip"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path"
	"sort"
	"strings"
	"sync"
)

// Server is a fake GitHub. Token is the only token it accepts.
type Server struct {
	*httptest.Server
	Token string
	Login string

	mu      sync.Mutex
	repos   map[string]*repo
	blobs   map[string][]byte
	trees   map[string]map[string]entry
	commits map[string]commit
	// Pushes counts successful ref updates, for tests that assert a no-op.
	Pushes int
	// DeviceApproveAfter is how many polls answer "pending" before a token.
	DeviceApproveAfter int
	polls              int
	// Refresh, when set, makes sign-in behave like a GitHub App's: the token
	// comes with this refresh token and an 8-hour expiry, and trading the
	// refresh token in replaces both (the old ones stop working).
	Refresh   string
	Refreshes int
	// AppSlug names the app's bot account (<slug>[bot]) served under /users.
	AppSlug string
	// Installations is what /user/installations reports.
	Installations int
	// Unverified, when set, is the reason GitHub gives for not verifying a
	// signed commit ("unknown_key", …); otherwise signed commits verify.
	Unverified string
	// Commits records each commit's author and committer, in order; read it
	// with Signatures while the server is running.
	Commits []Signature
}

// Signature is who a commit names, as the API received it, and whether it
// came signed.
type Signature struct {
	Author, Committer string // "name <email>", or "" when left to GitHub
	Signed            bool
}

// UserID and BotID are the account ids the fake hands out.
const (
	UserID = 1001
	BotID  = 2002
)

type repo struct {
	private bool
	refs    map[string]string
}

type entry struct {
	mode, typ, sha string
}

type commit struct {
	tree    string
	parents []string
	message string
}

// New starts a fake GitHub accepting token for login.
func New(token, login string) *Server {
	s := &Server{
		Token: token, Login: login,
		repos: map[string]*repo{}, blobs: map[string][]byte{},
		trees: map[string]map[string]entry{}, commits: map[string]commit{},
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

// AddRepo creates a repository directly (e.g. a public one, to test refusal).
func (s *Server) AddRepo(full string, private bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.repos[full] = s.newRepo(private)
}

// AddEmptyRepo creates a repository with no commits, as GitHub does when one
// is made without a README.
func (s *Server) AddEmptyRepo(full string, private bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.repos[full] = &repo{private: private, refs: map[string]string{}}
}

// Signatures returns who each commit so far named, oldest first.
func (s *Server) Signatures() []Signature {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Signature(nil), s.Commits...)
}

// File returns a file's content at the tip of a branch, for assertions.
func (s *Server) File(full, branch, p string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.repos[full]
	if r == nil {
		return nil, false
	}
	t := s.commits[r.refs[branch]].tree
	for _, part := range strings.Split(p, "/") {
		e, ok := s.trees[t][part]
		if !ok {
			return nil, false
		}
		if e.typ == "blob" {
			return s.blobs[e.sha], true
		}
		t = e.sha
	}
	return nil, false
}

func (s *Server) newRepo(private bool) *repo {
	readme := s.putBlob([]byte("# backup\n"))
	tree := s.putTree(map[string]entry{"README.md": {"100644", "blob", readme}})
	c := s.putCommit(commit{tree: tree, message: "Initial commit"})
	return &repo{private: private, refs: map[string]string{"main": c}}
}

func hash(kind string, b []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "%s %d\x00", kind, len(b))
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

func (s *Server) putBlob(b []byte) string {
	sha := hash("blob", b)
	s.blobs[sha] = b
	return sha
}

func (s *Server) putTree(t map[string]entry) string {
	names := make([]string, 0, len(t))
	for n := range t {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		fmt.Fprintf(&b, "%s %s %s %s\n", t[n].mode, t[n].typ, t[n].sha, n)
	}
	sha := hash("tree", []byte(b.String()))
	s.trees[sha] = t
	return sha
}

func (s *Server) putCommit(c commit) string {
	sha := hash("commit", []byte(c.tree+strings.Join(c.parents, ",")+c.message))
	s.commits[sha] = c
	return sha
}

// setPath writes e at the nested path p inside tree t, returning the new tree.
func (s *Server) setPath(t string, p string, e *entry) string {
	cur := map[string]entry{}
	for k, v := range s.trees[t] {
		cur[k] = v
	}
	head, rest, nested := strings.Cut(p, "/")
	if !nested {
		if e == nil {
			delete(cur, head)
		} else {
			cur[head] = *e
		}
		return s.putTree(cur)
	}
	sub := ""
	if old, ok := cur[head]; ok && old.typ == "tree" {
		sub = old.sha
	}
	cur[head] = entry{"040000", "tree", s.setPath(sub, rest, e)}
	return s.putTree(cur)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch r.URL.Path {
	case "/login/device/code":
		writeJSON(w, 200, map[string]any{"device_code": "dev-code", "user_code": "WDJB-MJHT",
			"verification_uri": s.URL + "/login/device", "expires_in": 900, "interval": 1})
		return
	case "/login/oauth/access_token":
		_ = r.ParseForm()
		if r.PostForm.Get("grant_type") == "refresh_token" {
			if s.Refresh == "" || r.PostForm.Get("refresh_token") != s.Refresh {
				writeJSON(w, 200, map[string]string{"error": "bad_refresh_token"})
				return
			}
			s.Refreshes++
			s.Token = fmt.Sprintf("%s-renewed%d", strings.Split(s.Token, "-renewed")[0], s.Refreshes)
			s.Refresh = fmt.Sprintf("%s-renewed%d", strings.Split(s.Refresh, "-renewed")[0], s.Refreshes)
			writeJSON(w, 200, s.tokenReply())
			return
		}
		s.polls++
		if s.polls <= s.DeviceApproveAfter {
			writeJSON(w, 200, map[string]string{"error": "authorization_pending"})
			return
		}
		writeJSON(w, 200, s.tokenReply())
		return
	}

	if r.Header.Get("Authorization") != "Bearer "+s.Token {
		writeJSON(w, 401, map[string]string{"message": "Bad credentials"})
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")

	switch {
	case r.URL.Path == "/user":
		writeJSON(w, 200, map[string]any{"login": s.Login, "id": UserID})
	case r.URL.Path == "/user/installations":
		writeJSON(w, 200, map[string]any{"total_count": s.Installations, "installations": []any{}})
	case s.AppSlug != "" && r.URL.Path == "/users/"+s.AppSlug+"[bot]":
		writeJSON(w, 200, map[string]any{"login": s.AppSlug + "[bot]", "id": BotID, "type": "Bot"})
	case r.URL.Path == "/user/repos" && r.Method == http.MethodPost:
		var req struct {
			Name     string `json:"name"`
			Private  bool   `json:"private"`
			AutoInit bool   `json:"auto_init"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		full := s.Login + "/" + req.Name
		if _, ok := s.repos[full]; ok {
			writeJSON(w, 422, map[string]string{"message": "name already exists on this account"})
			return
		}
		if req.AutoInit {
			s.repos[full] = s.newRepo(req.Private)
		} else {
			s.repos[full] = &repo{private: req.Private, refs: map[string]string{}}
		}
		writeJSON(w, 201, s.repoJSON(full))
	case len(parts) >= 3 && parts[0] == "repos":
		full := parts[1] + "/" + parts[2]
		rp := s.repos[full]
		if rp == nil {
			writeJSON(w, 404, map[string]string{"message": "Not Found"})
			return
		}
		s.repoRoute(w, r, full, rp, parts[3:])
	default:
		writeJSON(w, 404, map[string]string{"message": "Not Found"})
	}
}

type person struct{ Name, Email string }

func (p *person) String() string {
	if p == nil {
		return ""
	}
	return p.Name + " <" + p.Email + ">"
}

func (s *Server) tokenReply() map[string]any {
	m := map[string]any{"access_token": s.Token, "token_type": "bearer"}
	if s.Refresh != "" {
		m["refresh_token"], m["expires_in"], m["refresh_token_expires_in"] = s.Refresh, 28800, 15897600
	}
	return m
}

func (s *Server) repoJSON(full string) map[string]any {
	return map[string]any{"full_name": full, "private": s.repos[full].private, "default_branch": "main",
		"html_url": "https://github.com/" + full}
}

func (s *Server) repoRoute(w http.ResponseWriter, r *http.Request, full string, rp *repo, rest []string) {
	join := strings.Join(rest, "/")
	// Like GitHub: the Git Data API refuses a repository with no commits;
	// only the Contents API can create the first one.
	if len(rp.refs) == 0 && strings.HasPrefix(join, "git/") {
		writeJSON(w, 409, map[string]string{"message": "Git Repository is empty."})
		return
	}
	switch {
	case len(rest) == 0:
		writeJSON(w, 200, s.repoJSON(full))
	case strings.HasPrefix(join, "contents/") && r.Method == http.MethodPut:
		var req struct {
			Message, Content, Branch string
			Author, Committer        *person
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		s.Commits = append(s.Commits, Signature{Author: req.Author.String(), Committer: req.Committer.String()})
		b, err := base64.StdEncoding.DecodeString(req.Content)
		if err != nil {
			writeJSON(w, 400, map[string]string{"message": "bad base64"})
			return
		}
		if req.Branch == "" {
			req.Branch = "main"
		}
		base := s.putTree(map[string]entry{})
		var parents []string
		if tip, ok := rp.refs[req.Branch]; ok {
			base, parents = s.commits[tip].tree, []string{tip}
		}
		t := s.setPath(base, strings.TrimPrefix(join, "contents/"), &entry{"100644", "blob", s.putBlob(b)})
		c := s.putCommit(commit{t, parents, req.Message})
		rp.refs[req.Branch] = c
		writeJSON(w, 201, map[string]any{"commit": map[string]string{"sha": c}})
	case join == "git/refs" && r.Method == http.MethodPost:
		var req struct{ Ref, SHA string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		rp.refs[strings.TrimPrefix(req.Ref, "refs/heads/")] = req.SHA
		s.Pushes++
		writeJSON(w, 201, map[string]any{})
	case strings.HasPrefix(join, "git/ref/heads/") && r.Method == http.MethodGet:
		sha, ok := rp.refs[strings.TrimPrefix(join, "git/ref/heads/")]
		if !ok {
			writeJSON(w, 404, map[string]string{"message": "Not Found"})
			return
		}
		writeJSON(w, 200, map[string]any{"object": map[string]string{"sha": sha}})
	case strings.HasPrefix(join, "git/commits/") && r.Method == http.MethodGet:
		c, ok := s.commits[strings.TrimPrefix(join, "git/commits/")]
		if !ok {
			writeJSON(w, 404, map[string]string{"message": "Not Found"})
			return
		}
		writeJSON(w, 200, map[string]any{"tree": map[string]string{"sha": c.tree}})
	case join == "git/blobs" && r.Method == http.MethodPost:
		var req struct{ Content, Encoding string }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Encoding != "base64" {
			writeJSON(w, 400, map[string]string{"message": "bad blob"})
			return
		}
		b, err := base64.StdEncoding.DecodeString(req.Content)
		if err != nil {
			writeJSON(w, 400, map[string]string{"message": "bad base64"})
			return
		}
		writeJSON(w, 201, map[string]string{"sha": s.putBlob(b)})
	case join == "git/trees" && r.Method == http.MethodPost:
		var req struct {
			Base string `json:"base_tree"`
			Tree []struct {
				Path, Mode, Type string
				SHA              *string
				Content          *string
			}
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		t := req.Base
		if t == "" {
			t = s.putTree(map[string]entry{})
		}
		for _, e := range req.Tree {
			switch {
			case e.Content != nil:
				t = s.setPath(t, e.Path, &entry{e.Mode, "blob", s.putBlob([]byte(*e.Content))})
			case e.SHA == nil:
				t = s.setPath(t, e.Path, nil)
			default:
				t = s.setPath(t, e.Path, &entry{e.Mode, e.Type, *e.SHA})
			}
		}
		writeJSON(w, 201, map[string]string{"sha": t})
	case join == "git/commits" && r.Method == http.MethodPost:
		var req struct {
			Message           string
			Tree              string
			Parents           []string
			Author, Committer *person
			Signature         string
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		signed := req.Signature != ""
		s.Commits = append(s.Commits, Signature{req.Author.String(), req.Committer.String(), signed})
		verification := map[string]any{"verified": false, "reason": "unsigned"}
		if signed {
			verification = map[string]any{"verified": s.Unverified == "", "reason": cmp.Or(s.Unverified, "valid")}
		}
		writeJSON(w, 201, map[string]any{"sha": s.putCommit(commit{req.Tree, req.Parents, req.Message}), "verification": verification})
	case strings.HasPrefix(join, "git/refs/heads/") && r.Method == http.MethodPatch:
		var req struct{ SHA string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		branch := strings.TrimPrefix(join, "git/refs/heads/")
		c := s.commits[req.SHA]
		if len(c.parents) != 1 || c.parents[0] != rp.refs[branch] {
			writeJSON(w, 422, map[string]string{"message": "Update is not a fast forward"})
			return
		}
		rp.refs[branch] = req.SHA
		s.Pushes++
		writeJSON(w, 200, map[string]any{})
	case strings.HasPrefix(join, "tarball/"):
		s.tarball(w, full, rp.refs[strings.TrimPrefix(join, "tarball/")])
	case strings.HasPrefix(join, "contents/"):
		s.contents(w, r, rp, strings.TrimPrefix(join, "contents/"))
	default:
		writeJSON(w, 404, map[string]string{"message": "Not Found"})
	}
}

func (s *Server) lookup(rp *repo, p string) (entry, bool) {
	e := entry{"040000", "tree", s.commits[rp.refs["main"]].tree}
	for _, part := range strings.Split(p, "/") {
		if part == "" {
			continue
		}
		next, ok := s.trees[e.sha][part]
		if !ok {
			return entry{}, false
		}
		e = next
	}
	return e, true
}

func (s *Server) contents(w http.ResponseWriter, r *http.Request, rp *repo, p string) {
	e, ok := s.lookup(rp, p)
	if !ok {
		writeJSON(w, 404, map[string]string{"message": "Not Found"})
		return
	}
	if e.typ == "blob" {
		w.WriteHeader(200)
		_, _ = w.Write(s.blobs[e.sha])
		return
	}
	var out []map[string]string
	for name, child := range s.trees[e.sha] {
		typ := "file"
		if child.typ == "tree" {
			typ = "dir"
		}
		out = append(out, map[string]string{"name": name, "type": typ})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["name"] < out[j]["name"] })
	writeJSON(w, 200, out)
}

func (s *Server) tarball(w http.ResponseWriter, full, sha string) {
	w.Header().Set("Content-Type", "application/x-gzip")
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	root := strings.ReplaceAll(full, "/", "-") + "-" + sha[:7]
	var walk func(tree, dir string)
	walk = func(tree, dir string) {
		for name, e := range s.trees[tree] {
			p := path.Join(dir, name)
			if e.typ == "tree" {
				walk(e.sha, p)
				continue
			}
			mode := int64(0o644)
			if e.mode == "100755" {
				mode = 0o755
			}
			b := s.blobs[e.sha]
			_ = tw.WriteHeader(&tar.Header{Name: p, Mode: mode, Size: int64(len(b)), Typeflag: tar.TypeReg})
			_, _ = tw.Write(b)
		}
	}
	walk(s.commits[sha].tree, root)
	_ = tw.Close()
	_ = gz.Close()
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
