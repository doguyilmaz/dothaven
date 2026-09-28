package cli

import (
	"context"
	"sync"
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
