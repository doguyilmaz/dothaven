package cli

import (
	"strings"
	"testing"

	"github.com/doguyilmaz/dothaven/internal/snapshot"
)

func TestSlimSections(t *testing.T) {
	long := strings.Repeat("line\n", 20) // 21 fields when split on "\n"
	short := "a\nb\nc"
	s := snapshot.Snapshot{
		"big":   {Content: &long},
		"small": {Content: &short},
		"items": {Items: []snapshot.Item{{Raw: "x"}}}, // no content → untouched
	}

	slimSections(s)

	got := *s["big"].Content
	if !strings.Contains(got, "... (") || !strings.HasSuffix(got, "more lines)") {
		t.Errorf("big content not truncated: %q", got)
	}
	if lines := strings.Count(got, "\n"); lines != slimMaxLines {
		t.Errorf("truncated content has %d newlines, want %d", lines, slimMaxLines)
	}
	if *s["small"].Content != short {
		t.Error("short content should be untouched")
	}
	if s["items"].Content != nil {
		t.Error("content-less section should stay content-less")
	}
}

func TestDefaultCollectorsWired(t *testing.T) {
	if got := len(defaultCollectors()); got != 13 {
		t.Errorf("defaultCollectors length = %d, want 13 (meta + registry + 11 command collectors)", got)
	}
}
