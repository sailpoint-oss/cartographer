package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	// Set via ldflags at build time.
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("cartographer %s (commit: %s, built: %s)\n", Version, Commit, BuildDate)
	},
}
