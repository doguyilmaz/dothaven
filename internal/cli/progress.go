package cli

import (
	"fmt"
	"os"
	"sync/atomic"
	"time"
)

// startProgress streams a throttled count to stderr while slow work runs, and
// returns a func that stops it and clears the line.
//
// `collect` took five seconds against a real machine and printed nothing until
// it was done, which reads as a hang rather than as work. Anything that can run
// longer than about a second says so.
//
// Silent when stderr isn't a terminal, so piped and CI output stays clean. Pass
// total = 0 when the amount of work isn't known up front (a directory walk).
func startProgress(label string, done *int64, total int) func() {
	if !stderrIsTTY() {
		return func() {}
	}
	stop := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		t := time.NewTicker(150 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				n := atomic.LoadInt64(done)
				if total > 0 {
					fmt.Fprintf(os.Stderr, "\r\033[K%s… %d/%d", label, n, total)
				} else {
					fmt.Fprintf(os.Stderr, "\r\033[K%s… %d", label, n)
				}
			}
		}
	}()
	return func() {
		close(stop)
		<-finished // let the ticker stop before clearing, so no late tick re-prints
		fmt.Fprint(os.Stderr, "\r\033[K")
	}
}
