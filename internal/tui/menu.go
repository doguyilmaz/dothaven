package tui

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

// Node is one entry of the menu: a group that opens more entries, or an action
// (Value) the caller runs.
type Node struct {
	Label    string
	About    string // shown below the list while the entry is selected
	Value    string
	Children []Node
}

// Place is where the menu was left, so it opens there again after an action.
type Place struct {
	Path   []int // the groups opened, as indexes
	Cursor int
}

// The terminal's own palette (green, faint), not fixed colours: it is made
// to read on the terminal's background, and choosing between a light and a
// dark shade would mean asking the terminal for its background, which waits
// for an answer some terminals never give.
var (
	accent    = lipgloss.Color("2")
	titleSty  = lipgloss.NewStyle().Bold(true)
	crumbSty  = lipgloss.NewStyle().Bold(true).Foreground(accent)
	pickedSty = lipgloss.NewStyle().Bold(true).Foreground(accent)
	mutedSty  = lipgloss.NewStyle().Faint(true)
)

type statusMsg string

type menuModel struct {
	title    string
	status   string
	statusFn func() string
	root     []Node
	path     []int
	cursor   int
	width    int
	height   int
	sized    bool
	chosen   string
	done     bool
}

// Picked is what the menu was left with: the action ("" when it was left),
// where it was, and what a page for the action shows at its top.
type Picked struct {
	Value  string
	At     Place
	Crumb  []string // Home, the groups opened, the entry picked
	Status string
}

// RunMenu shows the menu at the top of a fresh page and returns what was
// picked. status, run once in the background, fills the top right corner
// (which machine, whether GitHub is signed in).
//
// The menu is not on the terminal's alternate screen: switching back to the
// normal screen for each action would drop the reader at the bottom of their
// shell history every time. The session is one page instead: the menu is
// wiped when an action starts, the action runs from the top of the window
// under the same header, and its output scrolls into the terminal's history
// when the menu comes back, where it can still be read.
func RunMenu(title string, status func() string, root []Node, at Place) (Picked, error) {
	NewPage()
	m := newMenu(title, status, root, at)
	out, err := tea.NewProgram(m).Run()
	ClearPage()
	if err != nil {
		return Picked{At: at}, err
	}
	fm := out.(menuModel)
	return Picked{Value: fm.chosen, At: fm.place(), Crumb: fm.crumb(true), Status: fm.status}, nil
}

// NewPage starts a fresh page: what is on the screen scrolls up into the
// terminal's history (still there to scroll back to), and the cursor goes to
// the top left corner.
func NewPage() {
	if !isTTY(os.Stdout) {
		return
	}
	fmt.Print(strings.Repeat("\n", termRows()) + "\x1b[H\x1b[J")
}

// ClearPage wipes the screen without keeping it, for the menu itself.
func ClearPage() {
	if isTTY(os.Stdout) {
		fmt.Print("\x1b[H\x1b[J")
	}
}

func termRows() int {
	if _, h, err := term.GetSize(int(os.Stdout.Fd())); err == nil && h > 0 {
		return h
	}
	return 24
}

// PageTop is the top of every page in the session, the menu's and each
// action's: the title with the status on the right, then where you are.
func PageTop(title, status string, crumb []string, width int) string {
	w := max(width, 30)
	var b strings.Builder
	line := func(s string) {
		b.WriteString(ansi.Truncate(s, w-1, "…"))
		b.WriteByte('\n')
	}
	left := " " + titleSty.Render(title)
	right := mutedSty.Render(status) + " "
	if gap := w - 1 - lipgloss.Width(left) - lipgloss.Width(right); gap < 2 || status == "" {
		line(left)
	} else {
		line(left + strings.Repeat(" ", gap) + right)
	}
	b.WriteByte('\n')
	line(" " + crumbSty.Render(strings.Join(crumb, " › ")))
	b.WriteByte('\n')
	return b.String()
}

func newMenu(title string, status func() string, root []Node, at Place) menuModel {
	m := menuModel{title: title, statusFn: status, root: root, width: 80, height: 24}
	// A saved place may no longer exist (the tree differs per OS); keep what
	// still does.
	for _, i := range at.Path {
		if i < 0 || i >= len(m.items()) || len(m.items()[i].Children) == 0 {
			break
		}
		m.path = append(m.path, i)
	}
	if at.Cursor >= 0 && at.Cursor < len(m.items()) {
		m.cursor = at.Cursor
	}
	return m
}

// crumb is Home and the groups opened, and with picked, the entry under the
// cursor.
func (m menuModel) crumb(picked bool) []string {
	crumb := []string{"Home"}
	level := m.root
	for _, i := range m.path {
		crumb = append(crumb, level[i].Label)
		level = level[i].Children
	}
	if picked && m.cursor < len(level) {
		crumb = append(crumb, level[m.cursor].Label)
	}
	return crumb
}

func (m menuModel) place() Place {
	return Place{Path: append([]int(nil), m.path...), Cursor: m.cursor}
}

// items is the level on screen.
func (m menuModel) items() []Node {
	level := m.root
	for _, i := range m.path {
		level = level[i].Children
	}
	return level
}

func (m menuModel) Init() tea.Cmd {
	if m.statusFn == nil {
		return nil
	}
	fn := m.statusFn
	return func() tea.Msg { return statusMsg(fn()) }
}

func (m menuModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		// A resize reflows what is on screen; drawing afresh from the top
		// keeps the page in place.
		if m.sized {
			return m, tea.ClearScreen
		}
		m.sized = true
	case statusMsg:
		m.status = string(msg)
	case tea.KeyMsg:
		return m.key(msg.String())
	}
	return m, nil
}

func (m menuModel) key(k string) (tea.Model, tea.Cmd) {
	items := m.items()
	switch k {
	case "up", "k", "shift+tab":
		m.cursor = (m.cursor - 1 + len(items)) % len(items)
	case "down", "j", "tab":
		m.cursor = (m.cursor + 1) % len(items)
	case "home", "g":
		m.cursor = 0
	case "end", "G":
		m.cursor = len(items) - 1
	case "enter", "right", "l", " ":
		return m.open()
	case "esc", "left", "h", "backspace":
		if len(m.path) == 0 {
			if k == "esc" {
				m.done = true
				return m, tea.Quit
			}
			return m, nil
		}
		m.cursor = m.path[len(m.path)-1]
		m.path = m.path[:len(m.path)-1]
	case "q", "ctrl+c":
		m.done = true
		return m, tea.Quit
	default:
		// 1-9 pick an entry by its number.
		if len(k) == 1 && k[0] >= '1' && k[0] <= '9' {
			if i := int(k[0] - '1'); i < len(items) {
				m.cursor = i
				return m.open()
			}
		}
	}
	return m, nil
}

func (m menuModel) open() (tea.Model, tea.Cmd) {
	n := m.items()[m.cursor]
	if len(n.Children) > 0 {
		m.path = append(m.path, m.cursor)
		m.cursor = 0
		return m, nil
	}
	if n.Value == "" {
		return m, nil
	}
	m.chosen, m.done = n.Value, true
	return m, tea.Quit
}

// View draws the menu to fit the window: every level is short enough to show
// whole, so nothing scrolls under the cursor.
func (m menuModel) View() string {
	if m.done {
		return ""
	}
	w := max(m.width, 30)
	var b strings.Builder
	line := func(s string) {
		b.WriteString(ansi.Truncate(s, w-1, "…"))
		b.WriteByte('\n')
	}
	b.WriteString(PageTop(m.title, m.status, m.crumb(false), w))
	level := m.items()

	for i, n := range level {
		label := n.Label
		if len(n.Children) > 0 {
			label += mutedSty.Render("  ›")
		}
		if i == m.cursor {
			line(" " + pickedSty.Render("▸ "+n.Label) + trailing(n))
		} else {
			line("   " + label)
		}
	}
	b.WriteByte('\n')
	line(" " + mutedSty.Render(strings.Repeat("─", min(w-2, 60))))
	about := level[m.cursor].About
	for _, l := range wrap(about, min(w-2, 72)) {
		line(" " + l)
	}
	keys := "↑↓ move · enter open · esc back · q quit"
	if len(m.path) == 0 {
		keys = "↑↓ move · enter open · q quit"
	}
	line(" " + mutedSty.Render(keys))
	return b.String()
}

func trailing(n Node) string {
	if len(n.Children) > 0 {
		return mutedSty.Render("  ›")
	}
	return ""
}

// wrap breaks s into lines of at most w characters, at spaces.
func wrap(s string, w int) []string {
	var out []string
	cur := ""
	for _, word := range strings.Fields(s) {
		if cur != "" && len(cur)+1+len(word) > w {
			out = append(out, cur)
			cur = word
			continue
		}
		if cur != "" {
			cur += " "
		}
		cur += word
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
