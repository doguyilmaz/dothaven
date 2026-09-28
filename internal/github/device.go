package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DeviceCode is what the user types into the browser, and what the terminal
// polls with while they do.
type DeviceCode struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// Scope is what the token may do: create and write the private backup repo.
// GitHub OAuth scopes are coarse — `repo` covers every private repository —
// so the docs point the careful at a fine-grained token limited to the one
// repository instead (`dothaven github login --with-token`).
const Scope = "repo"

// ErrDenied is the user pressing Cancel in the browser.
var ErrDenied = errors.New("sign-in was cancelled in the browser")

// ErrExpired is the code timing out before it was entered.
var ErrExpired = errors.New("the sign-in code expired — run login again")

// StartDeviceFlow asks GitHub for a sign-in code.
func (c *Client) StartDeviceFlow(ctx context.Context, clientID string) (DeviceCode, error) {
	var dc DeviceCode
	err := c.form(ctx, "/login/device/code", url.Values{"client_id": {clientID}, "scope": {Scope}}, &dc)
	if err == nil && (dc.DeviceCode == "" || dc.UserCode == "") {
		err = errors.New("GitHub did not return a sign-in code (is device flow enabled for this app?)")
	}
	return dc, err
}

// minPollInterval is the floor under GitHub's requested interval (a var so
// tests need not wait five seconds a poll).
var minPollInterval = 5 * time.Second

// Token is what a sign-in hands back. A GitHub App's token lasts 8 hours and
// comes with a refresh token good for 6 months; an OAuth app's token, a
// personal token and the GitHub CLI's have neither and do not expire.
type Token struct {
	Access         string    `json:"access"`
	Refresh        string    `json:"refresh,omitempty"`
	Expires        time.Time `json:"expires,omitzero"`
	RefreshExpires time.Time `json:"refresh_expires,omitzero"`
}

// Stale reports a token at or within five minutes of its expiry — close
// enough that a push starting now could outlive it.
func (t Token) Stale(now time.Time) bool {
	return !t.Expires.IsZero() && !now.Before(t.Expires.Add(-5*time.Minute))
}

// tokenReply is the access-token endpoint's answer, for both the device flow
// and a refresh; errors come back as 200 with "error" set.
type tokenReply struct {
	AccessToken           string `json:"access_token"`
	RefreshToken          string `json:"refresh_token"`
	ExpiresIn             int    `json:"expires_in"`
	RefreshTokenExpiresIn int    `json:"refresh_token_expires_in"`
	Error                 string `json:"error"`
	Interval              int    `json:"interval"`
}

func (r tokenReply) token(now time.Time) Token {
	t := Token{Access: r.AccessToken, Refresh: r.RefreshToken}
	if r.ExpiresIn > 0 {
		t.Expires = now.Add(time.Duration(r.ExpiresIn) * time.Second)
	}
	if r.RefreshTokenExpiresIn > 0 {
		t.RefreshExpires = now.Add(time.Duration(r.RefreshTokenExpiresIn) * time.Second)
	}
	return t
}

// ErrSignInExpired is a refresh token GitHub no longer accepts: six months
// passed, the app was revoked, or the token was already used.
var ErrSignInExpired = errors.New("your GitHub sign-in has expired — run: dothaven github login")

// RefreshToken trades a refresh token for a new pair. The old refresh token
// stops working, so the caller must keep the new one. A token from the
// device flow needs no client secret to refresh.
func (c *Client) RefreshToken(ctx context.Context, clientID, refresh string) (Token, error) {
	var r tokenReply
	err := c.form(ctx, "/login/oauth/access_token", url.Values{
		"client_id":     {clientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refresh},
	}, &r)
	switch {
	case err != nil:
		return Token{}, err
	case r.Error == "bad_refresh_token":
		return Token{}, ErrSignInExpired
	case r.Error != "":
		return Token{}, fmt.Errorf("renewing the GitHub sign-in failed: %s", r.Error)
	case r.AccessToken == "":
		return Token{}, errors.New("GitHub returned no token")
	}
	return r.token(time.Now()), nil
}

// WaitForToken polls until the user has approved the code in the browser,
// honouring GitHub's interval and its requests to slow down, and gives up when
// the code expires or ctx is cancelled.
func (c *Client) WaitForToken(ctx context.Context, clientID string, dc DeviceCode) (Token, error) {
	interval := max(time.Duration(dc.Interval)*time.Second, minPollInterval)
	deadline := time.Now().Add(time.Duration(max(dc.ExpiresIn, 60)) * time.Second)
	for {
		select {
		case <-ctx.Done():
			return Token{}, ctx.Err()
		case <-time.After(interval):
		}
		if time.Now().After(deadline) {
			return Token{}, ErrExpired
		}
		var r tokenReply
		err := c.form(ctx, "/login/oauth/access_token", url.Values{
			"client_id":   {clientID},
			"device_code": {dc.DeviceCode},
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		}, &r)
		if err != nil {
			return Token{}, err
		}
		switch r.Error {
		case "":
			if r.AccessToken == "" {
				return Token{}, errors.New("GitHub returned no token")
			}
			return r.token(time.Now()), nil
		case "authorization_pending":
		case "slow_down":
			interval = max(time.Duration(r.Interval)*time.Second, interval+5*time.Second)
		case "expired_token":
			return Token{}, ErrExpired
		case "access_denied":
			return Token{}, ErrDenied
		default:
			return Token{}, fmt.Errorf("sign-in failed: %s", r.Error)
		}
	}
}

// form posts to github.com (not the API host), as the OAuth endpoints live
// there.
func (c *Client) form(ctx context.Context, path string, v url.Values, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Web+path, strings.NewReader(v.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "dothaven")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return readError(resp)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}
