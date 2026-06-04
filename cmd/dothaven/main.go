// Command dothaven inventories a machine's dev configuration, scans for
// secrets, and feeds chezmoi (age-encrypted) for migration across machines.
package main

import (
	"fmt"
	"os"

	"github.com/doguyilmaz/dothaven/internal/cli"
	"github.com/doguyilmaz/dothaven/internal/sys"
)

// version is overridden at release time via -ldflags.
var version = "dev"

func main() {
	root := cli.NewRoot(sys.Real(), version)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
