package cmd

import (
	"github.com/olemoudi/kawarimi/internal/recipient"
	"github.com/spf13/cobra"
)

var openLang string

func init() {
	openCmd.Flags().StringVar(&openLang, "lang", "", "Language: es or en (prompts if not set)")
	rootCmd.AddCommand(openCmd)
}

var openCmd = &cobra.Command{
	Use:   "open",
	Short: "Open a received vault package (guided wizard for recipients)",
	Long: `Guided, plain-language wizard for a family member who received a vault package.
Finds the vault (or extracts the package zip) in the current folder, asks for the
key from the email and the words from the card, and writes the decrypted files to
a "decrypted" folder.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return recipient.Run(recipient.Options{Lang: openLang})
	},
}
