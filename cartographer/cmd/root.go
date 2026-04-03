package cmd

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "cartographer",
	Short: "OpenAPI service extraction tooling",
	Long: `Cartographer is the extraction-side CLI for single-service OpenAPI generation.

Use 'cartographer extract' to generate an OpenAPI spec from a service's source code.
Use 'cartographer init' to scaffold .cartographer/cartographer.yaml for a service.

Run 'cartographer help <command>' for details on any command.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		initLogger()
	},
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable debug logging")
	rootCmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "Suppress info logging (warnings and errors only)")

	rootCmd.AddCommand(extractCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(completionCmd)
	rootCmd.AddCommand(versionCmd)
}
