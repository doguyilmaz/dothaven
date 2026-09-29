package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/huh"
)

type keysReader struct{ chunks []string }

func (r *keysReader) Read(b []byte) (int, error) {
	if len(r.chunks) == 0 {
		return 0, errors.New("no more input")
	}
	n := copy(b, r.chunks[0])
	r.chunks = r.chunks[1:]
	return n, nil
}

// A passphrase is typed with the keys people expect, and Esc or Ctrl-C
// leaves it; an arrow key is not taken for Esc.
func TestReadSecretKeys(t *testing.T) {
	for _, c := range []struct {
		name   string
		chunks []string
		want   string
		abort  bool
	}{
		{"typed", []string{"correct horse\r"}, "correct horse", false},
		{"backspace", []string{"ab\x7fc\r"}, "ac", false},
		{"backspace over ş", []string{"aş\x7f\r"}, "a", false},
		{"ctrl-u", []string{"wrong\x15right\r"}, "right", false},
		{"arrow keys ignored", []string{"a", "\x1b[D", "b\r"}, "ab", false},
		{"esc", []string{"half", "\x1b"}, "", true},
		{"ctrl-c", []string{"half\x03"}, "", true},
	} {
		got, err := readSecretKeys(&keysReader{chunks: c.chunks})
		if c.abort != errors.Is(err, ErrAborted) || (!c.abort && (err != nil || got != c.want)) {
			t.Errorf("%s: %q, %v", c.name, got, err)
		}
	}
}

// In the menu, leaving a question leaves the action: its work is stopped and
// later questions are not asked. Outside the menu nothing is carried over.
func TestLeavingAnAction(t *testing.T) {
	stopped := false
	BeginAction(func() { stopped = true })
	answered()
	leave()
	if !stopped || !leaving() {
		t.Fatal("leaving did not stop the action")
	}
	if err := ask(huh.NewForm(huh.NewGroup(huh.NewNote()))); !errors.Is(err, ErrAborted) {
		t.Errorf("a question after leaving was asked: %v", err)
	}
	if left, atFirst := EndAction(); !left || atFirst {
		t.Errorf("left %v at first %v, want left after an answer", left, atFirst)
	}

	BeginAction(nil)
	leave()
	if left, atFirst := EndAction(); !left || !atFirst {
		t.Errorf("left at the first question: %v %v", left, atFirst)
	}

	leave() // outside the menu
	if leaving() || BackVerb() != "cancel" {
		t.Error("outside the menu, leaving carried over")
	}
}

// Every question's help line says how to leave it.
func TestHelpSaysEscBack(t *testing.T) {
	BeginAction(nil)
	defer EndAction()
	var help []string
	for _, k := range (withBack{huh.NewConfirm()}).KeyBinds() {
		help = append(help, k.Help().Key+" "+k.Help().Desc)
	}
	if !strings.Contains(strings.Join(help, ","), "esc back") {
		t.Errorf("help = %v", help)
	}
}
