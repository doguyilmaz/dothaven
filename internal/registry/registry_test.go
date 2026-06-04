package registry

import (
	"runtime"
	"strings"
	"testing"

	"github.com/doguyilmaz/dothaven/internal/sys"
)

func TestResolvePath(t *testing.T) {
	e := Entry{Paths: map[string]string{runtime.GOOS: "~/.zshrc"}}
	if got := ResolvePath(e, "/home/u"); got != "/home/u/.zshrc" {
		t.Errorf("ResolvePath = %q", got)
	}
	if got := ResolvePath(Entry{Paths: map[string]string{"plan9": "x"}}, "/h"); got != "" {
		t.Errorf("unknown platform should yield empty, got %q", got)
	}
}

func TestEntriesInvariants(t *testing.T) {
	idx := map[string]Entry{}
	for _, e := range Entries {
		if e.ID == "" || e.BackupDest == "" {
			t.Errorf("entry missing ID/BackupDest: %+v", e)
		}
		if _, dup := idx[e.ID]; dup {
			t.Errorf("duplicate entry ID: %s", e.ID)
		}
		if len(e.Paths) == 0 {
			t.Errorf("%s: no platform paths", e.ID)
		}
		idx[e.ID] = e
	}

	// Credential-bearing sources MUST be High: opaque tokens won't match a scan
	// pattern, so High is what forces encryption on export and exclusion from a
	// plaintext backup. This invariant stops a future entry from leaking.
	mustHigh := []string{
		"cloud.azure", "cloud.oci", "cloud.digitalocean", "cloud.fly", "cloud.linode",
		"cloud.hetzner", "cloud.vercel", "cloud.netlify", "cloud.supabase", "cloud.stripe",
		"cloud.railway", "cloud.terraform", "cloud.pulumi", "cloud.cloudflared",
		"cloud.aws.credentials", "cloud.kube.config", "cloud.docker.config",
		"secrets.netrc", "secrets.vault", "secrets.gnupg", "db.pgpass", "db.mycnf",
		"build.maven", "build.gradle", "npm.config",
	}
	for _, id := range mustHigh {
		e, ok := idx[id]
		if !ok {
			t.Errorf("expected credential entry %q to exist", id)
			continue
		}
		if e.Sensitivity != High {
			t.Errorf("%q must be High sensitivity (got %q) — credential files must be encrypted", id, e.Sensitivity)
		}
	}
}

func TestCollect(t *testing.T) {
	home := "/home/u"
	env := &sys.Fake{
		HomeDir: home,
		Files: map[string]string{
			home + "/.zshrc":                "alias ll='ls -la'\n",
			home + "/.npmrc":                "//registry/:_authToken=npm_supersecret123\n",
			home + "/.gemini/settings.json": `{"theme":"dark","mcpServers":{"argent":true}}`,
			home + "/.p10k.zsh":             "l1\nl2\nl3",
		},
		Dirs: map[string][]string{
			home + "/.claude/skills": {"b.md", "a.md"},
		},
	}

	snap := Collect(env, home, true, Entries)

	// File → content
	if c := snap["shell.zshrc"].Content; c == nil || !strings.Contains(*c, "alias ll") {
		t.Errorf("shell.zshrc content: %v", snap["shell.zshrc"])
	}
	// File + redact rule → npm token masked
	if c := snap["npm.config"].Content; c == nil || strings.Contains(*c, "npm_supersecret123") || !strings.Contains(*c, "[REDACTED]") {
		t.Errorf("npm.config not redacted: %v", snap["npm.config"].Content)
	}
	// JSON-extract (fields=[]) → flattens all keys incl. nested object
	if g := snap["ai.gemini.settings"].Pairs; g["theme"] != "dark" || g["argent"] != "true" {
		t.Errorf("gemini pairs: %v", g)
	}
	// FileMetadata → exists + line count (no content)
	if m := snap["terminal.p10k"].Pairs; m["exists"] != "true" || m["lines"] != "3" {
		t.Errorf("p10k metadata: %v", m)
	}
	if snap["terminal.p10k"].Content != nil {
		t.Error("metadata entry should carry no content")
	}
	// Dir → sorted items
	items := snap["ai.claude.skills"].Items
	if len(items) != 2 || items[0].Raw != "a.md" || items[1].Raw != "b.md" {
		t.Errorf("claude skills items: %v", items)
	}
	// Missing entries are simply absent
	if _, ok := snap["cloud.aws.credentials"]; ok {
		t.Error("entry not on disk should be absent")
	}
}
