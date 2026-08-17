package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Colour is used for meaning, never for decoration. Four of them, each with one
// job, so a glance sorts the output before any of it is read:
//
//	red     something that would be lost, or is broken
//	yellow  something to deal with, but nothing is lost
//	green   confirmed fine
//	cyan    a command you can type
//
// Everything else is bold (a heading) or faint (detail you read second).
// A fifth colour would mean nothing, and a rainbow means the reader has to
// learn a legend before the output helps them.
var (
	styBold   = lipgloss.NewStyle().Bold(true)
	styDim    = lipgloss.NewStyle().Faint(true)
	styOK     = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styWarn   = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	styDanger = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	styCmd    = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
)

// colorOn reports whether to emit escape codes: only to a terminal, and never
// when NO_COLOR is set. A piped or redirected run has to stay parseable, and
// NO_COLOR is the agreed way to ask for that on a terminal too.
func colorOn() bool {
	if _, set := os.LookupEnv("NO_COLOR"); set {
		return false
	}
	return stdoutIsTTY()
}

func paint(s lipgloss.Style, text string) string {
	if !colorOn() {
		return text
	}
	return s.Render(text)
}

func bold(s string) string   { return paint(styBold, s) }
func dim(s string) string    { return paint(styDim, s) }
func good(s string) string   { return paint(styOK, s) }
func warn(s string) string   { return paint(styWarn, s) }
func danger(s string) string { return paint(styDanger, s) }

// kbd renders something the reader can type. Commands buried in prose are hard
// to pick out precisely when they matter most — at the end of output, when the
// question is "so what do I run now".
//
// Named kbd, not cmd: `cmd` is the cobra command in every RunE in this package,
// and `ok` is the most common variable name in Go. A helper worth using
// everywhere must not shadow the names everywhere already uses.
func kbd(s string) string { return paint(styCmd, s) }

// header separates one command's output from the next.
//
// Running two actions from the menu produced one undifferentiated wall: nothing
// said where the first ended, and nothing said which action had produced what.
// A titled rule costs one line and answers both.
func header(title string) string {
	const width = 64
	line := strings.Repeat("─", max(0, width-len([]rune(title))-3))
	return "\n" + bold("── "+title+" ") + dim(line) + "\n"
}

// ellipsize shortens a path from the middle so a column stays aligned. The two
// ends of a path carry the meaning — which project, which file — and a plain
// truncation drops the half that identifies it.
func ellipsize(s string, width int) string {
	r := []rune(s)
	if len(r) <= width || width < 8 {
		return s
	}
	keep := width - 1 // room for the ellipsis
	head := keep / 2
	tail := keep - head
	return string(r[:head]) + "…" + string(r[len(r)-tail:])
}

// shortenPath fits a path into width by dropping leading segments, not
// characters. The last segments name the repository; a middle cut through
// "EaaS.Frontend.CSMS/EaaS.Mobile.Trugo.Availability" leaves
// "EaaS.Fron…ile.Trugo.Availability", which is neither path nor name. Falling
// back to a character cut only matters when one segment alone is too long.
func shortenPath(p string, width int) string {
	if len([]rune(p)) <= width {
		return p
	}
	segs := strings.Split(p, "/")
	for i := 1; i < len(segs); i++ {
		candidate := "…/" + strings.Join(segs[i:], "/")
		if len([]rune(candidate)) <= width {
			return candidate
		}
	}
	return ellipsize(p, width)
}

// padTo left-aligns s in width columns, shortening it if it does not fit, so a
// long path cannot push every following column out of line.
func padTo(s string, width int) string {
	s = shortenPath(s, width)
	if pad := width - len([]rune(s)); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

// printHeader writes a section separator for an action run from the menu.
func printHeader(title string) { fmt.Println(header(title)) }
