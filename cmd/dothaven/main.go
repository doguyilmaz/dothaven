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
	// Ctrl-C / SIGTERM handling, two-tier so a command can never become
	// un-interruptible: the FIRST signal cancels the context (commands that
	// observe it — the scan walk, exec'd children via CommandContext — stop
	// gracefully); the SECOND forces an immediate exit so a command that ignores
	// the context still dies on a second Ctrl-C. (A plain signal.NotifyContext
	// would disable the default kill-on-SIGINT and leave such commands wedged.)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		first := <-sigCh
		// Said at once: work that is stuck (a read macOS is holding for a
		// privacy prompt) may take a while to notice the cancel, and a Ctrl-C
		// with no answer gets pressed twenty times.
		fmt.Fprintln(os.Stderr, "\nStopping… press Ctrl-C again to quit now.")
		cancel()
		<-sigCh // a second signal force-exits, even if a command ignores the context
		// No defer runs past os.Exit: remove decrypted temporary files here,
		// but give it a second at most; a quit must not wait on cleanup.
		done := make(chan struct{})
		go func() {
			sys.RemoveTempDirs()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(time.Second):
		}
		// Exit code reflects the signal that initiated shutdown (the cause), using
		// the conventional 128+signum so a supervisor can tell SIGINT (130) from
		// SIGTERM (143).
		code := 130
		if first == syscall.SIGTERM {
			code = 143
		}
		os.Exit(code)
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
