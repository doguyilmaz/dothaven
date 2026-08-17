package health

import (
	"context"
	"os"
	"os/exec"
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

// Uses .bashrc rather than .zshrc: bash is present on every machine this runs
// on, zsh is not on a stock Linux CI image. The zsh path is the same code with
// a different binary name, and the missing-validator case is covered below.
func TestDetectsBrokenShell(t *testing.T) {
	requireShell(t, "bash")
	d := t.TempDir()
	p := filepath.Join(d, ".bashrc")
	os.WriteFile(p, []byte("if [ -f foo ]; then\n  echo hi\n"), 0o644) // no fi
	r := Check(context.Background(), ExecRunner, p)
	if r.Status != Broken {
		t.Errorf("unterminated if should be Broken, got %v (%s)", r.Status, r.Detail)
	}
}

func TestAcceptsValidShell(t *testing.T) {
	requireShell(t, "bash")
	d := t.TempDir()
	p := filepath.Join(d, ".bashrc")
	os.WriteFile(p, []byte("export PATH=/usr/bin:$PATH\nalias ll='ls -la'\n"), 0o644)
	if r := Check(context.Background(), ExecRunner, p); r.Status != OK {
		t.Errorf("valid shell reported %v: %s", r.Status, r.Detail)
	}
}

// requireShell skips a test that needs a real interpreter rather than letting
// its absence look like a product failure.
func requireShell(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not installed on this machine", name)
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

// CI caught this on Linux, where zsh is not installed: `zsh -n .zshrc` failed
// to exec and every valid .zshrc was reported broken. A health check that
// invents faults costs you the habit of reading it — the same failure mode as a
// secret scanner that flags your shell theme.
func TestMissingValidatorIsUncheckedNotBroken(t *testing.T) {
	noZsh := func(context.Context, string, ...string) (string, error) {
		return "", &exec.Error{Name: "zsh", Err: exec.ErrNotFound}
	}
	d := t.TempDir()
	p := filepath.Join(d, ".zshrc")
	os.WriteFile(p, []byte("alias ll='ls -la'\n"), 0o644)

	r := Check(context.Background(), noZsh, p)
	if r.Status == Broken {
		t.Fatalf("a machine without zsh must not call the file broken: %s", r.Detail)
	}
	if r.Status != Unchecked {
		t.Errorf("Status = %v, want Unchecked", r.Status)
	}
}

// A validator that ran and rejected the file is still Broken.
func TestParserRejectionIsStillBroken(t *testing.T) {
	rejects := func(context.Context, string, ...string) (string, error) {
		return "line 3: parse error near `fi'", &exec.ExitError{}
	}
	d := t.TempDir()
	p := filepath.Join(d, ".zshrc")
	os.WriteFile(p, []byte("if [ -f x ]; then\n"), 0o644)

	r := Check(context.Background(), rejects, p)
	if r.Status != Broken {
		t.Errorf("Status = %v, want Broken", r.Status)
	}
	if r.Detail == "" {
		t.Error("the parser's complaint should reach the user")
	}
}
