package release

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/doguyilmaz/dothaven/internal/sys"
)

const (
	// LatestURL answers with a 302 whose Location names the newest release —
	// prereleases and drafts excluded, which matches what the tap ships given
	// GoReleaser's `prerelease: auto`.
	//
	// Deliberately github.com and not api.github.com. The API allows 60
	// unauthenticated requests an hour per IP address, and an IP address is
	// what a company shares behind one NAT — so on an office network the check
	// would fail permanently while looking exactly like "you are up to date".
	// This endpoint has no such ceiling, and the whole answer arrives in a
	// header, so no response body is ever read.
	LatestURL = "https://github.com/doguyilmaz/dothaven/releases/latest"

	// tagPrefix is the only Location that is accepted. The redirect target is
	// never requested, but it is still input from the network, so it is matched
	// whole rather than picked apart.
	tagPrefix = "https://github.com/doguyilmaz/dothaven/releases/tag/"

	// A version check is never worth making someone wait. Two seconds is long
	// enough for a working connection and short enough that a broken one is
	// indistinguishable from no check at all.
	fetchTimeout = 2 * time.Second
)

// Fetcher reports where a URL points, without going there.
//
// The seam is here, one method wide, rather than on sys.Env: sys.Env is
// implemented by sys.Fake and consumed by every collector, none of which should
// grow a network method they never call.
type Fetcher interface {
	Fetch(ctx context.Context, url string) (location string, err error)
}

// Checker answers "is there a newer version" using at most one request per TTL.
type Checker struct {
	Fetcher   Fetcher
	CachePath string
	TTL       time.Duration
	// Now is injectable so the cache's expiry is testable without sleeping.
	Now func() time.Time
}

// cacheFile is the whole of what dothaven remembers about update checks. It
// holds no identifier and nothing about the machine — a timestamp and a version
// string, so the answer to "have we looked today" survives a restart.
type cacheFile struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest"`
}

// Cached returns the last version seen, without touching the network.
func (c *Checker) Cached() string { return c.load().Latest }

// Latest returns the newest published version, consulting the network only when
// the cache has expired — or always, when force is set.
func (c *Checker) Latest(ctx context.Context, force bool) (string, error) {
	cf := c.load()
	if !force && c.now().Sub(cf.CheckedAt) < c.TTL {
		return cf.Latest, nil
	}

	// Stamp before the request, not after. A laptop that is offline or behind a
	// captive portal would otherwise fail and re-check on every single command;
	// recording the attempt means one failed lookup a day at worst.
	cf.CheckedAt = c.now()
	c.save(cf)

	if c.Fetcher == nil {
		return cf.Latest, errors.New("no fetcher configured")
	}
	location, err := c.Fetcher.Fetch(ctx, LatestURL)
	if err != nil {
		return cf.Latest, err
	}
	tag, err := tagFromLocation(location)
	if err != nil {
		return cf.Latest, err
	}
	cf.Latest = tag
	c.save(cf)
	return tag, nil
}

func (c *Checker) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// load reads the cache, treating every problem as "nothing known yet". A
// truncated or hand-edited file must cost a redundant check, never a command.
func (c *Checker) load() cacheFile {
	b, err := os.ReadFile(c.CachePath)
	if err != nil {
		return cacheFile{}
	}
	var cf cacheFile
	if err := json.Unmarshal(b, &cf); err != nil {
		return cacheFile{}
	}
	return cf
}

// save writes the cache through the same atomic writer everything else uses. A
// failure is dropped: the only cost is checking again next time.
func (c *Checker) save(cf cacheFile) {
	b, err := json.Marshal(cf)
	if err != nil {
		return
	}
	_ = sys.WriteFile(c.CachePath, string(b))
}

// tagFromLocation reads the version out of the redirect target, accepting only
// this repository's own tag URL and nothing else.
func tagFromLocation(location string) (string, error) {
	tag, ok := strings.CutPrefix(location, tagPrefix)
	if !ok || tag == "" || strings.ContainsAny(tag, "/?#") {
		return "", fmt.Errorf("unexpected release location %q", location)
	}
	return tag, nil
}

// HTTP is the real Fetcher: short timeout, no body, no redirect followed.
func HTTP(userAgent string) Fetcher { return httpFetcher{ua: userAgent} }

type httpFetcher struct{ ua string }

func (h httpFetcher) Fetch(ctx context.Context, url string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	// GitHub rejects requests without one. It carries the version and nothing
	// else — no machine identifier, no usage data.
	req.Header.Set("User-Agent", h.ua)

	client := &http.Client{
		// The redirect is the answer, so it is read and not followed. Following
		// it is how a version check turns into a request to a host nobody
		// vetted, and there is nothing at the other end that is wanted anyway.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 300 || resp.StatusCode > 399 {
		return "", fmt.Errorf("update check returned %s", resp.Status)
	}
	location := resp.Header.Get("Location")
	if location == "" {
		return "", errors.New("update check returned no location")
	}
	return location, nil
}
