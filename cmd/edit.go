package cmd

import (
	"fmt"
	"strings"

	"github.com/olemoudi/kawarimi/internal/vault"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(editCmd)
}

var editCmd = &cobra.Command{
	Use:   "edit <id-or-name>",
	Short: "Edit an existing entry",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		query := strings.Join(args, " ")

		v, err := openVault()
		if err != nil {
			return err
		}

		entry := v.Manifest.FindEntry(query)
		if entry == nil {
			return fmt.Errorf("entry not found: %s", query)
		}

		data, err := v.ShowEntry(entry)
		if err != nil {
			return err
		}

		var newContent []byte
		if entry.Category == vault.CategoryCredentials {
			newContent, err = editCredentialInEditor(data)
		} else {
			newContent, err = editInEditor(data)
		}
		if err != nil {
			return err
		}

		if err := v.UpdateEntry(entry, newContent); err != nil {
			return err
		}

		fmt.Printf("Updated: %s (%s)\n", entry.Title, entry.ID)
		return nil
	},
}
