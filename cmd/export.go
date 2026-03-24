package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(exportCmd)
}

var exportCmd = &cobra.Command{
	Use:   "export <output-directory>",
	Short: "Decrypt entire vault to a directory",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		outputDir := args[0]

		v, err := openVault()
		if err != nil {
			return err
		}

		if err := v.Export(outputDir); err != nil {
			return err
		}

		fmt.Printf("Vault exported to %s\n", outputDir)
		fmt.Printf("%d entries decrypted\n", len(v.Manifest.Entries))
		return nil
	},
}
