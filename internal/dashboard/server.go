// Package dashboard serves dothaven's local, read-only dashboard: one page in
// the browser that shows what is covered, what is backed up, what is applied
// and what is at risk.
//
// It is built to be safe to leave open on a laptop:
//
//   - it listens on the loopback interface only, on a random port;
//   - every request needs a one-time token, exchanged on the first visit for
//     an HttpOnly, SameSite=Strict cookie so it does not linger in the URL bar
//     or the browser history;
//   - the Host header must be the loopback address it is bound to, which
//     closes DNS rebinding (a web page elsewhere resolving its own name to
//     127.0.0.1 to reach this server);
//   - it only answers GET, never writes anything, and sends a strict CSP with
//     no external assets — the page works offline and cannot load code from
//     anywhere.
package dashboard

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

//go:embed web
var web embed.FS

// Source produces one panel's data. It is called with a deadline and its
// result is cached briefly, so a burst of reloads does not repeat slow work.
type Source func(ctx context.Context) (any, error)

// Server is a running dashboard.
type Server struct {
	URL   string // with the one-time token, to open in a browser
	ctx   context.Context
	token string
	addr  string
	srv   *http.Server
	ln    net.Listener

	mu    sync.Mutex
	cache map[string]cached
	calls map[string]*call
}

type cached struct {
	at  time.Time
	val []byte
}

type call struct {
	done chan struct{}
	val  []byte
	err  error
}

// cacheFor is how long a panel's answer is reused.
const cacheFor = 20 * time.Second

// sourceTimeout bounds one panel's work (a git walk of a big home folder).
const sourceTimeout = 90 * time.Second

// Start listens on 127.0.0.1 and serves the dashboard with the given panels,
// until ctx is done.
func Start(ctx context.Context, sources map[string]Source) (*Server, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		ln.Close()
		return nil, err
	}
	s := &Server{
		ctx:   ctx,
		token: hex.EncodeToString(b),
		addr:  ln.Addr().String(),
		ln:    ln,
		cache: map[string]cached{},
		calls: map[string]*call{},
	}
	s.URL = "http://" + s.addr + "/?k=" + s.token

	static, _ := fs.Sub(web, "web")
	mux := http.NewServeMux()
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(static))))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/api/")
		src, ok := sources[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		body, err := s.get(r.Context(), name, src, r.URL.Query().Get("fresh") == "1")
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/assets/icon.svg", http.StatusMovedPermanently)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		page, _ := web.ReadFile("web/index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(page)
	})

	s.srv = &http.Server{
		Handler:           s.guard(mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      2 * sourceTimeout,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	go func() { _ = s.srv.Serve(ln) }()
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.srv.Shutdown(sctx)
	}()
	return s, nil
}

// Close stops the server.
func (s *Server) Close() error { return s.srv.Close() }

const cookieName = "dothaven_session"

// guard is the security boundary: loopback Host, GET only, the token, and
// headers that keep the page from being framed, sniffed or cached.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")

		_, port, _ := net.SplitHostPort(s.addr)
		if r.Host != s.addr && r.Host != "localhost:"+port {
			http.Error(w, "wrong host", http.StatusMisdirectedRequest)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "read-only", http.StatusMethodNotAllowed)
			return
		}
		if k := r.URL.Query().Get("k"); k != "" {
			if !s.valid(k) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: cookieName, Value: s.token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		c, err := r.Cookie(cookieName)
		if err != nil || !s.valid(c.Value) {
			http.Error(w, "Open the link dothaven printed (it carries a one-time key).", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) valid(k string) bool {
	return subtle.ConstantTimeCompare([]byte(k), []byte(s.token)) == 1
}

// get returns a panel's JSON, from cache when fresh, and never runs the same
// source twice at once: concurrent requests wait for the one in flight.
func (s *Server) get(ctx context.Context, name string, src Source, fresh bool) ([]byte, error) {
	s.mu.Lock()
	if c, ok := s.cache[name]; ok && !fresh && time.Since(c.at) < cacheFor {
		s.mu.Unlock()
		return c.val, nil
	}
	if cl, ok := s.calls[name]; ok {
		s.mu.Unlock()
		select {
		case <-cl.done:
			return cl.val, cl.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	cl := &call{done: make(chan struct{})}
	s.calls[name] = cl
	s.mu.Unlock()

	go func() {
		// Detached from the request (a closed tab must not waste the work;
		// the next reload gets it from the cache), but not from the server:
		// stopping the dashboard stops its panels.
		sctx, cancel := context.WithTimeout(s.ctx, sourceTimeout)
		defer cancel()
		v, err := safeCall(sctx, src)
		if err == nil {
			cl.val, err = json.Marshal(v)
		}
		cl.err = err
		s.mu.Lock()
		delete(s.calls, name)
		if err == nil {
			s.cache[name] = cached{at: time.Now(), val: cl.val}
		}
		s.mu.Unlock()
		close(cl.done)
	}()
	select {
	case <-cl.done:
		return cl.val, cl.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// safeCall turns a panic in one panel into that panel's error.
func safeCall(ctx context.Context, src Source) (v any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panel failed: %v", r)
		}
	}()
	v, err = src(ctx)
	if err == nil && v == nil {
		err = errors.New("no data")
	}
	return v, err
}
