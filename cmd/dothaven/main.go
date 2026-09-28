// Command dothaven inventories a machine's dev configuration, scans for
// secrets, and feeds chezmoi (age-encrypted) for migration across machines.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/doguyilmaz/dothaven/internal/cli"
	"github.com/doguyilmaz/dothaven/internal/sys"
)

// version is overridden at release time via -ldflags.
var version = "dev"

func main() {
	// Ctrl-C and SIGTERM, in two tiers so a command can never become
	// uninterruptible: the first signal cancels the context (the walk, the
	// upload and exec'd children stop), and the second exits at once, so a
	// command that ignores the context still ends. A plain
	// signal.NotifyContext would leave such a command wedged.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		var first os.Signal
		for {
			first = <-sigCh
			// In the menu, Ctrl-C cancels the action that is running and the
			// menu comes back; a second one while it winds down quits.
			ended, ok := cli.CancelAction()
			if !ok {
				break
			}
			fmt.Fprintln(os.Stderr, "\nStopping… press Ctrl-C again to quit dothaven.")
			select {
			case <-ended:
				continue
			case first = <-sigCh:
			}
			forceExit(first)
		}
		// Said at once: work that is stuck (a read macOS is holding for a
		// privacy prompt) may take a while to notice the cancel, and a Ctrl-C
		// with no answer gets pressed twenty times.
		fmt.Fprintln(os.Stderr, "\nStopping… press Ctrl-C again to quit now.")
		cancel()
		<-sigCh // a second signal force-exits, even if a command ignores the context
		forceExit(first)
	}()

	if err := cli.Execute(ctx, sys.Real(), version); err != nil {
		var ee cli.ExitError
		if errors.As(err, &ee) {
			os.Exit(ee.Code)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// forceExit quits now, after at most a second of removing decrypted
// temporary files: no defer runs past os.Exit, and a quit must not wait on
// cleanup. The exit code is 128 plus the signal that started the shutdown, so
// a supervisor can tell SIGINT (130) from SIGTERM (143).
func forceExit(first os.Signal) {
	done := make(chan struct{})
	go func() {
		sys.RemoveTempDirs()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
	}
	code := 130
	if first == syscall.SIGTERM {
		code = 143
	}
	os.Exit(code)
}
