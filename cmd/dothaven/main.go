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
	// Ctrl-C / SIGTERM cancels the context, which cancels in-flight commands
	// (CommandContext SIGKILLs children) so the process exits cleanly without
	// orphaning subprocesses.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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
