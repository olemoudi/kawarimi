package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(verifyCmd)
}

var verifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Verify all vault entries can be decrypted",
	RunE: func(cmd *cobra.Command, args []string) error {
		v, err := openVault()
		if err != nil {
			return err
		}

		errs := v.Verify()
		if len(errs) == 0 {
			fmt.Printf("All %d entries verified OK\n", len(v.Manifest.Entries))
			return nil
		}

		for _, e := range errs {
			fmt.Printf("FAIL: %s\n", e)
		}
		return fmt.Errorf("%d of %d entries failed verification", len(errs), len(v.Manifest.Entries))
	},
}
