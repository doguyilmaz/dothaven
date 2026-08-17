package release

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// realLocation is what github.com answers /releases/latest with.
const realLocation = "https://github.com/doguyilmaz/dothaven/releases/tag/v0.5.0"

type fakeFetcher struct {
	location string
	err      error
	calls    int
}

func (f *fakeFetcher) Fetch(_ context.Context, _ string) (string, error) {
	f.calls++
	return f.location, f.err
}

func newChecker(t *testing.T, f Fetcher, now time.Time) *Checker {
	t.Helper()
	return &Checker{
		Fetcher:   f,
		CachePath: filepath.Join(t.TempDir(), "update-check.json"),
		TTL:       24 * time.Hour,
		Now:       func() time.Time { return now },
	}
}

func TestTagFromLocation(t *testing.T) {
	got, err := tagFromLocation(realLocation)
	if err != nil {
		t.Fatalf("tagFromLocation: %v", err)
	}
	if got != "v0.5.0" {
		t.Errorf("tagFromLocation = %q, want v0.5.0", got)
	}

	bad := []struct {
		name, in string
	}{
		{"empty", ""},
		{"no tag segment", "https://github.com/doguyilmaz/dothaven/releases"},
		{"empty tag", "https://github.com/doguyilmaz/dothaven/releases/tag/"},
		{"extra path", "https://github.com/doguyilmaz/dothaven/releases/tag/v0.5.0/files"},
		{"query string smuggled in", "https://github.com/doguyilmaz/dothaven/releases/tag/v0.5.0?x=1"},
		// The redirect target is never requested, but it is still attacker-shaped
		// input if GitHub ever answers with something unexpected. Only this
		// repository's own tag URL is accepted.
		{"another host", "https://evil.example.com/releases/tag/v9.9.9"},
		{"another repository", "https://github.com/someone/else/releases/tag/v9.9.9"},
		{"not a url", "v0.5.0"},
	}
	for _, b := range bad {
		t.Run(b.name, func(t *testing.T) {
			if _, err := tagFromLocation(b.in); err == nil {
				t.Errorf("tagFromLocation(%q) = nil error, want an error", b.in)
			}
		})
	}
}

func TestLatestFetchesAndCaches(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	f := &fakeFetcher{location: realLocation}
	c := newChecker(t, f, now)

	got, err := c.Latest(context.Background(), false)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if got != "v0.5.0" {
		t.Errorf("Latest = %q, want v0.5.0", got)
	}
	if f.calls != 1 {
		t.Errorf("fetcher called %d times, want 1", f.calls)
	}
	if got := c.Cached(); got != "v0.5.0" {
		t.Errorf("Cached = %q, want it persisted as v0.5.0", got)
	}
}

func TestLatestSkipsNetworkWhileCacheIsFresh(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	f := &fakeFetcher{location: realLocation}
	c := newChecker(t, f, now)

	if _, err := c.Latest(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Latest(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if f.calls != 1 {
		t.Errorf("fetcher called %d times, want 1 — the second call must be served from cache", f.calls)
	}

	// Past the TTL it checks again.
	c.Now = func() time.Time { return now.Add(25 * time.Hour) }
	if _, err := c.Latest(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if f.calls != 2 {
		t.Errorf("fetcher called %d times after TTL expiry, want 2", f.calls)
	}
}

func TestLatestForceIgnoresFreshCache(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	f := &fakeFetcher{location: realLocation}
	c := newChecker(t, f, now)

	if _, err := c.Latest(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Latest(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if f.calls != 2 {
		t.Errorf("fetcher called %d times, want 2 — an explicit upgrade must not trust the cache", f.calls)
	}
}

func TestFailedCheckIsNotRetriedEveryRun(t *testing.T) {
	// The timestamp is written before the request, so a laptop that is offline
	// or behind a captive portal pays for one failed lookup a day, not one on
	// every single command.
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	f := &fakeFetcher{err: errors.New("no route to host")}
	c := newChecker(t, f, now)

	if _, err := c.Latest(context.Background(), false); err == nil {
		t.Fatal("Latest = nil error on a failed fetch, want the error surfaced")
	}
	if _, err := c.Latest(context.Background(), false); err != nil {
		t.Fatalf("second call should be a silent cache hit, got %v", err)
	}
	if f.calls != 1 {
		t.Errorf("fetcher called %d times, want 1", f.calls)
	}
}

func TestCacheProblemsAreNotFatal(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	c := newChecker(t, &fakeFetcher{location: realLocation}, now)

	// No cache file yet.
	if got := c.Cached(); got != "" {
		t.Errorf("Cached with no file = %q, want empty", got)
	}

	// A truncated or hand-mangled cache must read as "nothing known", not panic.
	if err := os.WriteFile(c.CachePath, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := c.Cached(); got != "" {
		t.Errorf("Cached with corrupt file = %q, want empty", got)
	}
	if _, err := c.Latest(context.Background(), false); err != nil {
		t.Errorf("Latest over a corrupt cache = %v, want it to recover", err)
	}
}

func TestHTTPFetcher(t *testing.T) {
	t.Run("returns the redirect target without following it", func(t *testing.T) {
		var reached bool
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			reached = true
		}))
		defer target.Close()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("User-Agent") == "" {
				t.Error("request carried no User-Agent; GitHub rejects those")
			}
			http.Redirect(w, r, target.URL+"/releases/tag/v0.5.0", http.StatusFound)
		}))
		defer srv.Close()

		got, err := HTTP("dothaven/test").Fetch(context.Background(), srv.URL)
		if err != nil {
			t.Fatalf("Fetch: %v", err)
		}
		if got != target.URL+"/releases/tag/v0.5.0" {
			t.Errorf("Fetch = %q, want the Location header verbatim", got)
		}
		// Following the redirect is how a version check turns into a request to
		// a host nobody vetted.
		if reached {
			t.Error("the redirect was followed")
		}
	})

	t.Run("errors when the answer is not a redirect", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Write([]byte("<html>a release page</html>"))
		}))
		defer srv.Close()

		if _, err := HTTP("dothaven/test").Fetch(context.Background(), srv.URL); err == nil {
			t.Error("Fetch on a 200 = nil error, want an error")
		}
	})

	t.Run("errors on a redirect with no location", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusFound)
		}))
		defer srv.Close()

		if _, err := HTTP("dothaven/test").Fetch(context.Background(), srv.URL); err == nil {
			t.Error("Fetch on a locationless redirect = nil error, want an error")
		}
	})
}
