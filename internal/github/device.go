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

// WaitForToken polls until the user has approved the code in the browser,
// honouring GitHub's interval and its requests to slow down, and gives up when
// the code expires or ctx is cancelled.
func (c *Client) WaitForToken(ctx context.Context, clientID string, dc DeviceCode) (string, error) {
	interval := max(time.Duration(dc.Interval)*time.Second, minPollInterval)
	deadline := time.Now().Add(time.Duration(max(dc.ExpiresIn, 60)) * time.Second)
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(interval):
		}
		if time.Now().After(deadline) {
			return "", ErrExpired
		}
		var r struct {
			AccessToken string `json:"access_token"`
			Error       string `json:"error"`
			Interval    int    `json:"interval"`
		}
		err := c.form(ctx, "/login/oauth/access_token", url.Values{
			"client_id":   {clientID},
			"device_code": {dc.DeviceCode},
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		}, &r)
		if err != nil {
			return "", err
		}
		switch r.Error {
		case "":
			if r.AccessToken == "" {
				return "", errors.New("GitHub returned no token")
			}
			return r.AccessToken, nil
		case "authorization_pending":
		case "slow_down":
			interval = max(time.Duration(r.Interval)*time.Second, interval+5*time.Second)
		case "expired_token":
			return "", ErrExpired
		case "access_denied":
			return "", ErrDenied
		default:
			return "", fmt.Errorf("sign-in failed: %s", r.Error)
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
