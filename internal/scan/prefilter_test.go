package scan

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func bothWays(content string) (fast, slow Result) {
	fast = ScanContentFull("f", content)
	noPrefilter = true
	slow = ScanContentFull("f", content)
	noPrefilter = false
	return fast, slow
}

func sameResult(a, b Result) bool {
	ids := func(ps []Pattern) []string {
		var out []string
		for _, p := range ps {
			out = append(out, p.ID)
		}
		return out
	}
	strip := func(fs []Finding) []Finding {
		out := make([]Finding, len(fs))
		for i, f := range fs {
			f.Pattern = Pattern{ID: f.Pattern.ID}
			out[i] = f
		}
		return out
	}
	return a.Action == b.Action && reflect.DeepEqual(strip(a.Findings), strip(b.Findings)) && reflect.DeepEqual(ids(a.redact), ids(b.redact))
}

// The prefilter skips a rule only where it cannot match. Every file in this
// repository (test fixtures full of fake keys included) scans the same with
// it and without it.
func TestPrefilterChangesNothing(t *testing.T) {
	root := filepath.Join("..", "..")
	n := 0
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() && (d.Name() == ".git" || d.Name() == "public") {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil || len(b) > 2<<20 {
			return nil
		}
		fast, slow := bothWays(string(b))
		if !sameResult(fast, slow) {
			t.Errorf("%s: prefilter %+v, without %+v", p, fast.Findings, slow.Findings)
		}
		n++
		return nil
	})
	if n < 100 {
		t.Fatalf("only %d files compared", n)
	}
}

func FuzzPrefilterChangesNothing(f *testing.F) {
	for _, s := range []string{
		"export GITHUB_TOKEN=ghp_" + strings.Repeat("a", 36),
		"password: hunter2hunter2",
		"PASSWORD=x",
		"ſecret = abcdefgh",
		"DOCKER_HOST=tcp://10.2.3.4:2375",
		"postgres://u:p@h/db",
		"-----BEGIN OPENSSH PRIVATE KEY-----",
		"AKIAABCDEFGHIJKLMNOP",
		"Authorization: Bearer " + strings.Repeat("x", 30),
		"host:5432:db:user:pw",
		"me@example.com /Users/x/ ~/y",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if fast, slow := bothWays(s); !sameResult(fast, slow) {
			t.Fatalf("%q: prefilter %+v, without %+v", s, fast, slow)
		}
	})
}
