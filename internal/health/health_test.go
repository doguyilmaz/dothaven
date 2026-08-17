package health

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDetectsBrokenJSON(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "settings.json")
	os.WriteFile(p, []byte(`{"a": 1,}`), 0o644) // trailing comma
	r := Check(context.Background(), ExecRunner, p)
	if r.Status != Broken {
		t.Errorf("trailing comma should be Broken, got %v (%s)", r.Status, r.Detail)
	}
}

func TestAcceptsValidJSON(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "settings.json")
	os.WriteFile(p, []byte(`{"a": 1}`), 0o644)
	if r := Check(context.Background(), ExecRunner, p); r.Status != OK {
		t.Errorf("valid JSON reported %v: %s", r.Status, r.Detail)
	}
}

// Editors allow comments in their settings files; encoding/json does not.
// Calling that "broken" would be the tool being wrong, not the config.
func TestJSONWithCommentsIsNotBroken(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "settings.json")
	os.WriteFile(p, []byte("// editor settings\n{\"a\": 1}\n"), 0o644)
	if r := Check(context.Background(), ExecRunner, p); r.Status == Broken {
		t.Errorf("jsonc should not be Broken: %s", r.Detail)
	}
}

func TestDetectsBrokenShell(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, ".zshrc")
	os.WriteFile(p, []byte("if [ -f foo ]; then\n  echo hi\n"), 0o644) // no fi
	r := Check(context.Background(), ExecRunner, p)
	if r.Status != Broken {
		t.Errorf("unterminated if should be Broken, got %v (%s)", r.Status, r.Detail)
	}
}

func TestAcceptsValidShell(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, ".zshrc")
	os.WriteFile(p, []byte("export PATH=/usr/bin:$PATH\nalias ll='ls -la'\n"), 0o644)
	if r := Check(context.Background(), ExecRunner, p); r.Status != OK {
		t.Errorf("valid zsh reported %v: %s", r.Status, r.Detail)
	}
}

func TestUnknownFormatIsUncheckedNotOK(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "thing.conf")
	os.WriteFile(p, []byte("whatever\n"), 0o644)
	if r := Check(context.Background(), ExecRunner, p); r.Status != Unchecked {
		t.Errorf("a format with no parser must say so, got %v", r.Status)
	}
}
