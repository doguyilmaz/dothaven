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
		cancel()
		<-sigCh // a second signal force-exits, even if a command ignores the context
		// Exit code reflects the signal that initiated shutdown (the cause), using
		// the conventional 128+signum so a supervisor can tell SIGINT (130) from
		// SIGTERM (143).
		code := 130
		if first == syscall.SIGTERM {
			code = 143
		}
		os.Exit(code)
	}()

	root := cli.NewRoot(sys.Real(), version)
	if err := root.ExecuteContext(ctx); err != nil {
		var ee cli.ExitError
		if errors.As(err, &ee) {
			os.Exit(ee.Code)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
