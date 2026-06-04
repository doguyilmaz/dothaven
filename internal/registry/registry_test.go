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
