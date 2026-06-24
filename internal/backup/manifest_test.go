package backup

import (
	"strings"
	"testing"
)

func TestManifestDescribesAndExcludes(t *testing.T) {
	res := Result{
		TotalFiles:       5,
		PerCategory:      map[string]int{"shell": 3, "git": 2},
		SkippedSensitive: []string{"secrets/gnupg", "cloud/aws/credentials"},
	}
	m := Manifest(ManifestMeta{Host: "mac", OS: "darwin", Version: "v1.2.3", Created: "2026-06-24T00:00:00Z", Redacted: true}, res)
	for _, want := range []string{
		"host:     mac", "os:       darwin", "dothaven: v1.2.3",
		"yes (secrets redacted)", "dothaven restore <this-directory>",
		"shell (3)", "git (2)",
		"secrets/gnupg", "cloud/aws/credentials", "chezmoi-export --apply",
	} {
		if !strings.Contains(m, want) {
			t.Errorf("manifest missing %q:\n%s", want, m)
		}
	}
}

func TestManifestNoExclusions(t *testing.T) {
	m := Manifest(ManifestMeta{Host: "m", Redacted: false}, Result{PerCategory: map[string]int{}})
	if !strings.Contains(m, "Excluded: none") {
		t.Errorf("expected 'Excluded: none':\n%s", m)
	}
	if !strings.Contains(m, "no (raw values kept)") {
		t.Errorf("expected raw-values note:\n%s", m)
	}
}
