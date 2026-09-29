package tui

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"golang.org/x/term"
)

// Every question can be left with Esc (or Ctrl-C). In the menu, leaving a
// question leaves the action it belongs to: the questions after it are not
// asked, its work is stopped, and the menu comes back. Outside the menu it
// cancels the command, as Ctrl-C always has.
var session struct {
	sync.Mutex
	inMenu   bool
	answered int
	left     bool
	onLeave  func()
}

// BeginAction starts a menu action. onLeave stops its work when a question
// in it is left.
func BeginAction(onLeave func()) {
	session.Lock()
	defer session.Unlock()
	session.inMenu, session.answered, session.left, session.onLeave = true, 0, false, onLeave
}

// EndAction reports whether the action was left, and whether that happened
// at its first question (nothing answered yet).
func EndAction() (left, atFirst bool) {
	session.Lock()
	defer session.Unlock()
	left, atFirst = session.left, session.answered == 0
	session.inMenu, session.answered, session.left, session.onLeave = false, 0, false, nil
	return left, atFirst
}

// BackVerb is what Esc does here: "go back" in the menu, "cancel" outside it.
func BackVerb() string {
	session.Lock()
	defer session.Unlock()
	if session.inMenu {
		return "go back"
	}
	return "cancel"
}

func leaving() bool {
	session.Lock()
	defer session.Unlock()
	return session.left
}

func answered() {
	session.Lock()
	defer session.Unlock()
	session.answered++
}

func leave() {
	session.Lock()
	stop := session.onLeave
	if session.inMenu {
		session.left = true
	}
	session.Unlock()
	if stop != nil {
		stop()
	}
}

// Left reports whether a question in the running menu action was left.
func Left() bool { return leaving() }

// ask runs a form, counting answers and leaving on Esc or Ctrl-C. It runs
// the program itself: huh ends a cancelled form with an interrupt, which
// skips the last redraw and leaves the question on screen. Ending both ways
// with Quit clears it, as an answer does.
func ask(f *huh.Form) error {
	if leaving() {
		return ErrAborted
	}
	f.SubmitCmd, f.CancelCmd = tea.Quit, tea.Quit
	if _, err := tea.NewProgram(f).Run(); err != nil {
		return fmt.Errorf("huh: %w", err)
	}
	if f.State == huh.StateAborted {
		leave()
		return ErrAborted
	}
	answered()
	return nil
}

// keys is huh's keymap with Esc leaving the question, like Ctrl-C.
func keys(search bool) *huh.KeyMap {
	km := huh.NewDefaultKeyMap()
	km.Quit = key.NewBinding(key.WithKeys("ctrl+c", "esc"))
	km.Select.Filter.SetEnabled(search)
	km.MultiSelect.Filter.SetEnabled(search)
	return km
}

// withBack is a field whose help line also says how to leave it. huh lists
// only a field's own keys there, and Esc belongs to the form.
type withBack struct{ huh.Field }

func (b withBack) KeyBinds() []key.Binding {
	verb := "cancel"
	if BackVerb() == "go back" {
		verb = "back"
	}
	return append(b.Field.KeyBinds(), key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", verb)))
}

// Update keeps the wrapper: huh stores whatever a field's Update returns.
func (b withBack) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m, cmd := b.Field.Update(msg)
	if f, ok := m.(huh.Field); ok {
		b.Field = f
	}
	return b, cmd
}

// ReadSecret reads a passphrase from the terminal without echoing it. Esc
// or Ctrl-C leaves, as on any other question: term.ReadPassword keeps the
// terminal in line mode, where neither reaches the program until Enter.
// It reads /dev/tty rather than stdin, so it works with output piped.
func ReadSecret(prompt string) (string, error) {
	if leaving() {
		return "", ErrAborted
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", err
	}
	defer tty.Close()
	fd := int(tty.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		return "", err
	}
	defer func() { _ = term.Restore(fd, old) }()
	fmt.Fprint(tty, prompt)
	secret, err := readSecretKeys(tty)
	fmt.Fprint(tty, "\r\n")
	switch {
	case errors.Is(err, ErrAborted):
		leave()
	case err == nil:
		answered()
	}
	return secret, err
}

// readSecretKeys reads keys until Enter. Backspace deletes a character,
// Ctrl-U the whole line; a lone Esc or Ctrl-C leaves. An escape sequence
// (an arrow key) arrives in one read and is ignored.
func readSecretKeys(r interface{ Read([]byte) (int, error) }) (string, error) {
	var secret []byte
	buf := make([]byte, 256)
	for {
		n, err := r.Read(buf)
		if err != nil {
			return "", err
		}
		chunk := buf[:n]
		for i := 0; i < len(chunk); i++ {
			switch c := chunk[i]; {
			case c == '\r' || c == '\n':
				return string(secret), nil
			case c == 3:
				return "", ErrAborted
			case c == 0x1b:
				if i == len(chunk)-1 {
					return "", ErrAborted
				}
				i = skipEscape(chunk, i)
			case c == 0x7f || c == 8:
				if len(secret) > 0 {
					_, size := utf8.DecodeLastRune(secret)
					secret = secret[:len(secret)-size]
				}
			case c == 0x15:
				secret = secret[:0]
			case c < 0x20:
			default:
				secret = append(secret, c)
			}
		}
	}
}

// skipEscape returns the index of the last byte of the escape sequence
// starting at chunk[i] (ESC [ … final, or ESC O x).
func skipEscape(chunk []byte, i int) int {
	if i+1 >= len(chunk) {
		return i
	}
	switch chunk[i+1] {
	case '[':
		for j := i + 2; j < len(chunk); j++ {
			if chunk[j] >= 0x40 && chunk[j] <= 0x7e {
				return j
			}
		}
		return len(chunk) - 1
	case 'O':
		return min(i+2, len(chunk)-1)
	}
	return i + 1
}
