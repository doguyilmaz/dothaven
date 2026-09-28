package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A list line that wraps moves the whole list under the cursor, so every
// line must fit the window (80 columns off a terminal), and a note such as
// "credentials" must survive the cut.
func TestColumnsFitTheWindow(t *testing.T) {
	labels := []string{"Encrypted: everything, keys included", "Readable, with secrets encrypted", "ai"}
	hints := []string{
		"one file only your passphrase opens (recommended)",
		"browse and diff config on GitHub; credentials in an encrypted bundle, with a long tail that cannot fit",
		"Claude, Codex, Cursor, Gemini, skills, agents, MCP servers and plugins, all of them",
	}
	notes := []string{"", "", "🔑 credentials"}
	for _, indent := range []int{selectIndent, multiIndent} {
		lines := columns(labels, hints, notes, indent)
		for i, l := range lines {
			if w := ansi.StringWidth(l) + indent; w >= 80 {
				t.Errorf("indent %d, line %d is %d columns wide: %q", indent, i, w, ansi.Strip(l))
			}
			if !strings.HasPrefix(ansi.Strip(l), labels[i]) {
				t.Errorf("line %d lost its label: %q", i, ansi.Strip(l))
			}
		}
		if !strings.HasSuffix(ansi.Strip(lines[2]), "🔑 credentials") {
			t.Errorf("the note was cut: %q", ansi.Strip(lines[2]))
		}
		if !strings.Contains(ansi.Strip(lines[1]), "…") {
			t.Errorf("a hint too long for the window is not marked as cut: %q", ansi.Strip(lines[1]))
		}
	}
}
