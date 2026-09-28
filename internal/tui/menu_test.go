package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var tree = []Node{
	{Label: "Move to a new Mac", About: "Check, pack, restore.", Children: []Node{
		{Label: "Check nothing would be lost", Value: "ready", About: "Read-only."},
		{Label: "Pack everything", Value: "pack"},
		{Label: "Restore a backup", Value: "restore"},
	}},
	{Label: "GitHub backup", Children: []Node{
		{Label: "Save this Mac to GitHub", Value: "github push"},
		{Label: "Restore from GitHub", Value: "github pull"},
	}},
	{Label: "Help me choose", Value: "guide"},
	{Label: "Quit", Value: ""},
}

func press(m menuModel, keys ...string) menuModel {
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case "up":
			msg = tea.KeyMsg{Type: tea.KeyUp}
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		next, _ := m.Update(msg)
		m = next.(menuModel)
	}
	return m
}

func TestMenuNavigation(t *testing.T) {
	m := newMenu("dothaven", nil, tree, Place{})
	m = press(m, "down", "enter") // GitHub backup
	if len(m.path) != 1 || m.items()[0].Value != "github push" {
		t.Fatalf("did not open the group: path %v", m.path)
	}
	m = press(m, "down", "enter")
	if m.chosen != "github pull" || !m.done {
		t.Fatalf("chosen %q", m.chosen)
	}

	// Back out of a group lands on the group it came from.
	m = newMenu("dothaven", nil, tree, Place{})
	m = press(m, "down", "enter", "esc")
	if len(m.path) != 0 || m.cursor != 1 || m.done {
		t.Fatalf("after esc: path %v cursor %d done %v", m.path, m.cursor, m.done)
	}
	// Esc at the top leaves; up from the first entry wraps to the last.
	if m = press(newMenu("dothaven", nil, tree, Place{}), "up"); m.cursor != len(tree)-1 {
		t.Errorf("up from the top: cursor %d", m.cursor)
	}
	if m = press(m, "esc"); !m.done || m.chosen != "" {
		t.Error("esc at the top should leave with nothing chosen")
	}
	// Numbers pick; a group entry with no value does nothing.
	if m = press(newMenu("dothaven", nil, tree, Place{}), "1", "2"); m.chosen != "pack" {
		t.Errorf("1 then 2: chosen %q", m.chosen)
	}
	if m = press(newMenu("dothaven", nil, tree, Place{}), "4"); m.done {
		t.Error("an entry without a value must not end the menu")
	}
}

func TestMenuReopensWhereItWasLeft(t *testing.T) {
	m := press(newMenu("dothaven", nil, tree, Place{}), "enter", "down", "down", "enter")
	if m.chosen != "restore" {
		t.Fatalf("chosen %q", m.chosen)
	}
	again := newMenu("dothaven", nil, tree, m.place())
	if len(again.path) != 1 || again.cursor != 2 {
		t.Errorf("reopened at %v/%d", again.path, again.cursor)
	}
	// A place that no longer exists falls back to what does.
	if odd := newMenu("dothaven", nil, tree, Place{Path: []int{9}, Cursor: 7}); len(odd.path) != 0 || odd.cursor != 0 {
		t.Errorf("stale place: %v/%d", odd.path, odd.cursor)
	}
}

// The whole menu fits a small terminal: nothing scrolls under the cursor.
func TestMenuFitsTheWindow(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {40, 20}, {120, 40}} {
		m := newMenu("dothaven", nil, tree, Place{})
		next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = next.(menuModel)
		m = press(m, "enter")
		v := m.View()
		lines := strings.Split(strings.TrimRight(v, "\n"), "\n")
		if len(lines) > size[1] {
			t.Errorf("%dx%d: %d lines", size[0], size[1], len(lines))
		}
		for _, l := range lines {
			if w := lipgloss.Width(l); w >= size[0] {
				t.Errorf("%dx%d: line %d wide: %q", size[0], size[1], w, l)
			}
		}
		for _, want := range []string{"Home › Move to a new Mac", "Check nothing would be lost", "Read-only.", "esc back"} {
			if !strings.Contains(v, want) {
				t.Errorf("%dx%d: view misses %q:\n%s", size[0], size[1], want, v)
			}
		}
	}
}
