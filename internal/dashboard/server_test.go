package dashboard

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func start(t *testing.T, sources map[string]Source) *Server {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s, err := Start(ctx, sources)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestKeyExchangeAndGuards(t *testing.T) {
	s := start(t, map[string]Source{"summary": func(context.Context) (any, error) { return map[string]int{"files": 3}, nil }})
	u, _ := url.Parse(s.URL)
	if u.Hostname() != "127.0.0.1" {
		t.Fatalf("listening on %s, want loopback", u.Host)
	}
	base := "http://" + u.Host

	// No key, no cookie: refused.
	resp, _ := http.Get(base + "/api/summary")
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("without the key: %d", resp.StatusCode)
	}
	// A wrong key is refused, not traded for a cookie.
	resp, _ = http.Get(base + "/?k=nope")
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("wrong key: %d", resp.StatusCode)
	}

	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	resp, err := c.Get(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Request.URL.RawQuery != "" {
		t.Errorf("the key stayed in the URL after the exchange: %s", resp.Request.URL)
	}
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") || !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("CSP = %q", csp)
	}
	var session *http.Cookie
	for _, ck := range jar.Cookies(u) {
		if ck.Name == cookieName {
			session = ck
		}
	}
	if session == nil {
		t.Fatal("no session cookie")
	}
	resp, _ = c.Get(base + "/api/summary")
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"files":3`) {
		t.Errorf("api with cookie: %d %s", resp.StatusCode, body)
	}
	resp, _ = c.Get(base + "/assets/app.js")
	if resp.StatusCode != 200 {
		t.Errorf("assets: %d", resp.StatusCode)
	}

	// DNS rebinding: a request that reaches the port under another name.
	req, _ := http.NewRequest(http.MethodGet, base+"/api/summary", nil)
	req.Host = "evil.example:" + u.Port()
	req.AddCookie(&http.Cookie{Name: cookieName, Value: session.Value})
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusMisdirectedRequest {
		t.Errorf("foreign Host header: %d", resp.StatusCode)
	}

	// Read-only.
	req, _ = http.NewRequest(http.MethodPost, base+"/api/summary", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: session.Value})
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST: %d", resp.StatusCode)
	}
}

func authed(t *testing.T, s *Server) (*http.Client, string) {
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	if _, err := c.Get(s.URL); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(s.URL)
	return c, "http://" + u.Host
}

// A burst of reloads runs a slow panel once, not once per request.
func TestPanelsAreDeduplicatedAndCached(t *testing.T) {
	var calls int64
	s := start(t, map[string]Source{"slow": func(context.Context) (any, error) {
		atomic.AddInt64(&calls, 1)
		time.Sleep(100 * time.Millisecond)
		return "ok", nil
	}})
	c, base := authed(t, s)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			resp, err := c.Get(base + "/api/slow")
			if err == nil {
				resp.Body.Close()
			}
		})
	}
	wg.Wait()
	resp, _ := c.Get(base + "/api/slow")
	resp.Body.Close()
	if n := atomic.LoadInt64(&calls); n != 1 {
		t.Errorf("slow panel ran %d times, want 1", n)
	}
	resp, _ = c.Get(base + "/api/slow?fresh=1")
	resp.Body.Close()
	if n := atomic.LoadInt64(&calls); n != 2 {
		t.Errorf("fresh=1 should recompute: %d", n)
	}
}

func TestPanicIsOnePanelsError(t *testing.T) {
	s := start(t, map[string]Source{
		"bad":  func(context.Context) (any, error) { panic("boom") },
		"good": func(context.Context) (any, error) { return 1, nil },
	})
	c, base := authed(t, s)
	resp, _ := c.Get(base + "/api/bad")
	if resp.StatusCode != 500 {
		t.Errorf("panicking panel: %d", resp.StatusCode)
	}
	resp, _ = c.Get(base + "/api/good")
	if resp.StatusCode != 200 {
		t.Errorf("a panic took the server down: %d", resp.StatusCode)
	}
}
