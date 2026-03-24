package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(completionCmd)
}

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell completion script",
	Long: `Generate shell completion script for kawarimi.

To load completions:

Bash:
  $ source <(kawarimi completion bash)
  # To load completions for each session, execute once:
  $ kawarimi completion bash > /etc/bash_completion.d/kawarimi

Zsh:
  $ source <(kawarimi completion zsh)
  # To load completions for each session, execute once:
  $ kawarimi completion zsh > "${fpath[1]}/_kawarimi"

Fish:
  $ kawarimi completion fish | source
  # To load completions for each session, execute once:
  $ kawarimi completion fish > ~/.config/fish/completions/kawarimi.fish

PowerShell:
  PS> kawarimi completion powershell | Out-String | Invoke-Expression
`,
	Args:      cobra.ExactArgs(1),
	ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
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
		default:
			return cmd.Help()
		}
	},
}
