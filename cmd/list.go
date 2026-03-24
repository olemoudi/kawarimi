package cmd

import (
	"fmt"

	"github.com/olemoudi/kawarimi/internal/vault"
	"github.com/spf13/cobra"
)

func init() {
	listCmd.Flags().StringP("category", "c", "", "Filter by category (notes, credentials, documents)")
	rootCmd.AddCommand(listCmd)
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List vault entries",
	RunE: func(cmd *cobra.Command, args []string) error {
		v, err := openVault()
		if err != nil {
			return err
		}

		category, _ := cmd.Flags().GetString("category")

		var entries []*vault.Entry
		if category != "" {
			entries = v.Manifest.FindEntriesByCategory(vault.Category(category))
		} else {
			entries = v.Manifest.Entries
		}

		if len(entries) == 0 {
			fmt.Println("No entries found.")
			return nil
		}

		for _, e := range entries {
			fmt.Printf("[%s] %-12s %s\n", e.ID, e.Category, e.Title)
		}
		fmt.Printf("\n%d entries total\n", len(entries))

		return nil
	},
}
