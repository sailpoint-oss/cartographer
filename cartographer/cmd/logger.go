package cmd

import (
	"os"

	charlog "github.com/charmbracelet/log"
)

var (
	verbose bool
	quiet   bool
	logger  *charlog.Logger
)

func initLogger() {
	level := charlog.InfoLevel
	if verbose {
		level = charlog.DebugLevel
	} else if quiet {
		level = charlog.WarnLevel
	}
	logger = charlog.NewWithOptions(os.Stderr, charlog.Options{
		Level:           level,
		ReportTimestamp: true,
	})
}
