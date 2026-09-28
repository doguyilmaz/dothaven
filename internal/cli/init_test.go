package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
	"github.com/doguyilmaz/dothaven/internal/sys"
)

// A key made by hand is not enough: init's age step passes only once
// chezmoi.toml uses it, and what the user already wrote there stays.
func TestWriteAgeConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir()) // no chezmoi, and no gh to ask GitHub
	env := sys.Real()
	dir := filepath.Join(home, ".config", "chezmoi")
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	key := "# created: today\n# public key: " + id.Recipient().String() + "\n" + id.String() + "\n"
	if err := os.WriteFile(filepath.Join(dir, "key.txt"), []byte(key), 0o600); err != nil {
		t.Fatal(err)
	}
	old := "[data]\n    email = \"me@example.com\"\n"
	if err := os.WriteFile(filepath.Join(dir, "chezmoi.toml"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if probeInitState(t.Context(), env).AgeKeyConfigured {
		t.Fatal("configured before anything was written")
	}

	if err := writeAgeConfig(env); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "chezmoi.toml"))
	got := string(b)
	for _, want := range []string{`encryption = "age"`, old, `recipient = "` + id.Recipient().String() + `"`, filepath.Join(dir, "key.txt")} {
		if !strings.Contains(got, want) {
			t.Errorf("chezmoi.toml lacks %q:\n%s", want, got)
		}
	}
	if saved, _ := os.ReadFile(filepath.Join(dir, "chezmoi.toml.before-dothaven")); string(saved) != old {
		t.Errorf("the old file was not kept: %q", saved)
	}
	if !probeInitState(t.Context(), env).AgeKeyConfigured {
		t.Error("init still says age is not configured")
	}
	if err := writeAgeConfig(env); err == nil {
		t.Error("a second run changed a file that already sets encryption")
	}
}
