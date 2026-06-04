package tui

import (
	"strings"
	"testing"
)

func TestRenderDiff(t *testing.T) {
	out := RenderDiff("alpha\nbeta\ngamma", "alpha\nBETA\ngamma")
	if !strings.Contains(out, "- beta") || !strings.Contains(out, "+ BETA") {
		t.Errorf("diff missing the changed lines:\n%s", out)
	}
	if strings.Contains(out, "alpha") {
		t.Errorf("unchanged lines should not appear:\n%s", out)
	}
	if !strings.Contains(RenderDiff("same", "same"), "no textual differences") {
		t.Error("identical input should report no differences")
	}
}
