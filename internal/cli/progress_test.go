package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/doguyilmaz/dothaven/internal/sys"
)

func TestActivityLineFitsAndKeepsThePathEnd(t *testing.T) {
	long := "ai/claude/plugins/marketplaces/some-very-long-marketplace-name/plugins/x/skills/y/SKILL.md"
	line := activityLine("Reading your config", 23489, 0, func() string { return long }, 41*time.Second, 0)
	if utf8.RuneCountInString(line) > 79 {
		t.Errorf("line is %d wide: %q", utf8.RuneCountInString(line), line)
	}
	for _, want := range []string{"Reading your config", "23,489", "41s", "SKILL.md"} {
		if !strings.Contains(line, want) {
			t.Errorf("%q misses %q", line, want)
		}
	}
	if strings.Contains(activityLine("Uploading", 3, 10, nil, time.Second, 0), "1s") {
		t.Error("the time is shown only once a step passes two seconds")
	}
}

func TestRunLogKeepsTenAndRecordsNamesOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", "")
	env := sys.Real()
	dir := filepath.Join(env.CacheDir(), "logs")
	os.MkdirAll(dir, 0o700)
	for i := 0; i < 12; i++ {
		os.WriteFile(filepath.Join(dir, "2020-01-01T00-00-"+string(rune('a'+i))+"-x.log"), nil, 0o600)
	}
	l := &runLogger{}
	l.open(env, "dothaven github push")
	l.reading("ai/claude/plugins", false)
	l.command([]string{"brew", "list"}, 1200*time.Millisecond, nil)
	l.close(ExitError{Code: 130})
	entries, _ := os.ReadDir(dir)
	if len(entries) != keepLogs {
		t.Errorf("%d logs kept, want %d", len(entries), keepLogs)
	}
	b, err := os.ReadFile(l.Path())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"dothaven github push", "reading ai/claude/plugins", "ran brew list (1.2s, ok)", "exit code 130"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("log misses %q:\n%s", want, b)
		}
	}
	if fi, _ := os.Stat(l.Path()); fi.Mode().Perm() != 0o600 {
		t.Errorf("log mode %v, want 0600", fi.Mode().Perm())
	}
}
