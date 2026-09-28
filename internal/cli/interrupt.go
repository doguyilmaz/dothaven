package cli

import (
	"bytes"
	"context"
	"os"
	"sync"

	"github.com/muesli/cancelreader"
)

// The running menu action, so Ctrl-C can cancel just that action and leave
// the menu running.
var (
	actionMu     sync.Mutex
	actionCancel context.CancelFunc
	actionDone   chan struct{}
)

func startAction(cancel context.CancelFunc) {
	actionMu.Lock()
	defer actionMu.Unlock()
	actionCancel, actionDone = cancel, make(chan struct{})
}

func endAction() {
	actionMu.Lock()
	defer actionMu.Unlock()
	if actionDone != nil {
		close(actionDone)
	}
	actionCancel, actionDone = nil, nil
}

// CancelAction cancels the menu action that is running, if one is, and
// returns a channel closed when it has ended. ok is false when no action is
// running: the signal is then for the whole program.
func CancelAction() (ended <-chan struct{}, ok bool) {
	actionMu.Lock()
	defer actionMu.Unlock()
	if actionCancel == nil {
		return nil, false
	}
	actionCancel()
	return actionDone, true
}

// waitEnter returns when Enter is pressed on stdin, or when ctx ends. The
// read is cancelled with ctx: a read left running would take the next line
// typed, and the Enter meant for the prompt after it would vanish.
func waitEnter(ctx context.Context) {
	r, err := cancelreader.NewReader(os.Stdin)
	if err != nil {
		<-ctx.Done()
		return
	}
	defer r.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 256)
		for {
			n, err := r.Read(buf)
			if err != nil || n == 0 || bytes.ContainsAny(buf[:n], "\r\n") {
				return
			}
		}
	}()
	select {
	case <-done:
	case <-ctx.Done():
		r.Cancel()
		<-done
	}
}
