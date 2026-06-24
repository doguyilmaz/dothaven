package cli

import "testing"

func TestCuratedServiceConfigs(t *testing.T) {
	cfgs := curatedServiceConfigs()
	if len(cfgs) == 0 {
		t.Fatal("expected a curated service-config set")
	}
	seen := map[string]bool{}
	for _, c := range cfgs {
		if seen[c] {
			t.Errorf("duplicate %s", c)
		}
		seen[c] = true
	}
	if !seen["nginx/nginx.conf"] {
		t.Error("expected nginx/nginx.conf in the set")
	}
}

func TestRewriteServicePrefix(t *testing.T) {
	in := "error_log /opt/homebrew/var/log/nginx/error.log;"
	if got := rewriteServicePrefix(in, "/opt/homebrew", "/usr/local"); got != "error_log /usr/local/var/log/nginx/error.log;" {
		t.Errorf("rewrite = %q", got)
	}
	if rewriteServicePrefix(in, "/opt/homebrew", "/opt/homebrew") != in {
		t.Error("matching prefix should be a no-op")
	}
	if rewriteServicePrefix(in, "", "/usr/local") != in {
		t.Error("empty old prefix should be a no-op")
	}
}
