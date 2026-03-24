package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(showCmd)
}

var showCmd = &cobra.Command{
	Use:   "show <id-or-name>",
	Short: "Decrypt and display an entry",
	Args:  cobra.ExactArgs(1),
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

		fmt.Fprintf(os.Stderr, "--- %s [%s] (%s) ---\n", entry.Title, entry.ID, entry.Category)
		os.Stdout.Write(data)
		if len(data) > 0 && data[len(data)-1] != '\n' {
			fmt.Println()
		}

		return nil
	},
}
