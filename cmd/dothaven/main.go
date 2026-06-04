// Command dothaven inventories a machine's dev configuration, scans for
// secrets, and feeds chezmoi (age-encrypted) for migration across machines.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// version is overridden at release time via -ldflags.
var version = "dev"

func main() {
	root := &cobra.Command{
		Use:     "dothaven",
		Short:   "Discover, back up, and migrate your machine's dev config",
		Long:    "dothaven inventories your machine's dev configuration (AI tools, shell, git, editors,\nSSH, cloud CLIs, package managers, toolchains, fonts), scans for secrets, and feeds\nchezmoi (age-encrypted) for migration across machines.",
		Version: version,
		// Errors are returned by RunE on subcommands; don't dump usage on them.
		SilenceUsage: true,
	}

	// Subcommands are registered here as they are ported:
	//   root.AddCommand(newCollectCmd(), newScanCmd(), newChezmoiExportCmd(), ...)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
