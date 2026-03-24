package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func init() {
	removeCmd.Flags().BoolP("force", "f", false, "Skip confirmation")
	rootCmd.AddCommand(removeCmd)
}

var removeCmd = &cobra.Command{
	Use:     "remove <id-or-name>",
	Aliases: []string{"rm"},
	Short:   "Remove an entry from the vault",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		query := args[0]

		v, err := openVault()
		if err != nil {
			return err
		}

		entry := v.Manifest.FindEntry(query)
		if entry == nil {
			return fmt.Errorf("entry not found: %s", query)
		}

		force, _ := cmd.Flags().GetBool("force")
		if !force {
			fmt.Printf("Remove \"%s\" [%s] (%s)? (y/N): ", entry.Title, entry.ID, entry.Category)
			reader := bufio.NewReader(os.Stdin)
			answer, _ := reader.ReadString('\n')
			answer = strings.TrimSpace(strings.ToLower(answer))
			if answer != "y" && answer != "yes" {
				fmt.Println("Cancelled.")
				return nil
			}
		}

		removed, err := v.RemoveEntry(entry.ID)
		if err != nil {
			return err
		}

		fmt.Printf("Removed: %s (%s)\n", removed.Title, removed.ID)
		return nil
	},
}
