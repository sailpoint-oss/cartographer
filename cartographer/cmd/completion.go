package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell completion scripts",
	Long: `Generate shell completion scripts for cartographer.

Installation instructions:

  bash:
    cartographer completion bash > /etc/bash_completion.d/cartographer
    # or for the current user:
    cartographer completion bash > ~/.local/share/bash-completion/completions/cartographer

  zsh:
    cartographer completion zsh > "${fpath[1]}/_cartographer"
    # you may need to run: compinit

  fish:
    cartographer completion fish > ~/.config/fish/completions/cartographer.fish

  powershell:
    cartographer completion powershell | Out-String | Invoke-Expression
    # or add to your profile:
    cartographer completion powershell >> $PROFILE`,
	ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
	Args:                  cobra.ExactValidArgs(1),
	DisableFlagsInUseLine: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return rootCmd.GenBashCompletion(os.Stdout)
		case "zsh":
			return rootCmd.GenZshCompletion(os.Stdout)
		case "fish":
			return rootCmd.GenFishCompletion(os.Stdout, true)
		case "powershell":
			return rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
		}
		return nil
	},
}
