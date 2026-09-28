package cli

import (
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"golang.org/x/term"
)

// startProgress streams a throttled count to stderr while slow work runs, and
// returns a func that stops it and clears the line. Anything that can run
// longer than about a second says so: silence reads as a hang.
//
// Silent when stderr isn't a terminal, so piped and CI output stays clean. Pass
// total = 0 when the amount of work isn't known up front (a directory walk).
func startProgress(label string, done *int64, total int) func() {
	return startActivity(label, done, total, nil)
}

// startActivity is startProgress with a detail (the folder or file being
// worked on) and the time taken so far, so a stall shows where it is.
func startActivity(label string, done *int64, total int, detail func() string) func() {
	if !stderrIsTTY() {
		return func() {}
	}
	stop := make(chan struct{})
	finished := make(chan struct{})
	start := time.Now()
	go func() {
		defer close(finished)
		t := time.NewTicker(150 * time.Millisecond)
		defer t.Stop()
		spin := 0
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				spin++
				fmt.Fprint(os.Stderr, "\r\033[K"+activityLine(label, atomic.LoadInt64(done), total, detail, time.Since(start), spin))
			}
		}
	}()
	return func() {
		close(stop)
		<-finished // let the ticker stop before clearing, so no late tick re-prints
		fmt.Fprint(os.Stderr, "\r\033[K")
	}
}

var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// activityLine is one status line, cut to the terminal width: a line that
// wraps can no longer be redrawn in place.
func activityLine(label string, n int64, total int, detail func() string, took time.Duration, spin int) string {
	parts := []string{spinner[spin%len(spinner)] + " " + label}
	switch {
	case total > 0:
		parts = append(parts, fmt.Sprintf("%d/%d", n, total))
	case n > 0:
		parts = append(parts, groupDigits(n))
	}
	if took >= 2*time.Second {
		parts = append(parts, took.Round(time.Second).String())
	}
	line := strings.Join(parts, " · ")
	w := termWidth() - 1
	if detail != nil {
		if d := detail(); d != "" {
			// The end of a path says most, so a long one loses its start.
			room := w - utf8.RuneCountInString(line) - 3
			if room > 10 {
				line += " · " + keepTail(d, room)
			}
		}
	}
	return fitWidth(line, w)
}

func keepTail(s string, w int) string {
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	return "…" + string(r[len(r)-w+1:])
}

func termWidth() int {
	if w, _, err := term.GetSize(int(os.Stderr.Fd())); err == nil && w > 20 {
		return w
	}
	return 80
}

// fitWidth cuts s to w characters.
func fitWidth(s string, w int) string {
	if utf8.RuneCountInString(s) <= w {
		return s
	}
	r := []rune(s)
	return string(r[:w-1]) + "…"
}

func groupDigits(n int64) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
