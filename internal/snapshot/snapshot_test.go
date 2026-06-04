package snapshot

import (
	"reflect"
	"strings"
	"testing"
)

func ptr(s string) *string { return &s }

func TestSerialize(t *testing.T) {
	s := Snapshot{
		"runtimes.go":         {Pairs: map[string]string{"version": "go1.26.3"}},
		"packages.bun.global": {Items: []Item{{Raw: "eas-cli@16.19.2", Columns: []string{"eas-cli", "16.19.2"}}}},
		"shell.zshrc":         {Content: ptr("alias ll='ls -la'\n")},
		"empty.section":       {},
	}
	out, err := s.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)

	if !strings.HasSuffix(got, "\n") {
		t.Error("want trailing newline")
	}
	for _, want := range []string{
		`  "runtimes.go": {`,       // 2-space indent
		`"version": "go1.26.3"`,    // pairs
		`"raw": "eas-cli@16.19.2"`, // items
		`"empty.section": {}`,      // all-empty section omits every field
		`alias ll='ls -la'`,        // content value kept
	} {
		if !strings.Contains(got, want) {
			t.Errorf("serialize missing %q in:\n%s", want, got)
		}
	}
	// alphabetical key order (deterministic): empty.section < packages < runtimes < shell
	order := []string{"empty.section", "packages.bun.global", "runtimes.go", "shell.zshrc"}
	last := -1
	for _, k := range order {
		i := strings.Index(got, `"`+k+`"`)
		if i < last {
			t.Errorf("keys not in alphabetical order: %q appeared before expected", k)
		}
		last = i
	}
}

func TestSerializeNoHTMLEscape(t *testing.T) {
	s := Snapshot{"x": {Pairs: map[string]string{"url": "https://h/?a=1&b=2", "v": "node>=18"}}}
	out, _ := s.Serialize()
	got := string(out)
	for _, raw := range []string{"https://h/?a=1&b=2", "node>=18"} {
		if !strings.Contains(got, raw) {
			t.Errorf("value should be unescaped, missing %q in %s", raw, got)
		}
	}
	// Go escapes to \uXXXX (not HTML entities); none must appear with escaping off
	if strings.Contains(got, `\u00`) {
		t.Errorf("HTML escaping leaked (\\uXXXX form present): %s", got)
	}
}

func TestSerializeOmitEmptyVsEmptyContent(t *testing.T) {
	out, _ := Snapshot{
		"nilc":   {},                 // content nil → omitted
		"emptyc": {Content: ptr("")}, // content "" → kept
	}.Serialize()
	got := string(out)
	if !strings.Contains(got, `"nilc": {}`) {
		t.Errorf("nil-content section should serialize to {}: %s", got)
	}
	if !strings.Contains(got, `"content": ""`) {
		t.Errorf("empty-string content should be kept: %s", got)
	}
}

func TestParseRoundTrip(t *testing.T) {
	orig := Snapshot{
		"a.b": {
			Pairs:   map[string]string{"k1": "v1", "k2": "v2"},
			Items:   []Item{{Raw: "pkg@2.0.0", Columns: []string{"pkg", "2.0.0"}}},
			Content: ptr("line1\nline2\n"),
		},
	}
	out, err := orig.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	back, err := Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(orig, back) {
		t.Errorf("round-trip mismatch:\n got %#v\nwant %#v", back, orig)
	}
}

func TestParseDefaults(t *testing.T) {
	s, err := Parse([]byte(`{"runtimes.go":{"pairs":{"version":"go1.26.3"}},"x":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := s["runtimes.go"].Pairs["version"]; got != "go1.26.3" {
		t.Errorf("pairs not parsed: %q", got)
	}
	if s["x"].Pairs != nil || s["x"].Items != nil || s["x"].Content != nil {
		t.Errorf("missing fields should default to nil: %#v", s["x"])
	}
}

func TestParseRejectsNonObject(t *testing.T) {
	for _, bad := range []string{`not json`, `[1,2,3]`, `42`, `"str"`} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}
