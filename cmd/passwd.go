package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/olemoudi/kawarimi/internal/crypto"
	"github.com/olemoudi/kawarimi/internal/vault"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(passwdCmd)
}

var passwdCmd = &cobra.Command{
	Use:   "passwd",
	Short: "Change the vault passphrase",
	RunE: func(cmd *cobra.Command, args []string) error {
		v, err := openVault()
		if err != nil {
			return err
		}

		fmt.Println("Set a new passphrase for the vault.")
		newPassphrase, err := crypto.PromptPassphraseConfirm()
		if err != nil {
			return err
		}

		// Re-encrypt every file with the new passphrase
		for i, entry := range v.Manifest.Entries {
			data, err := v.ShowEntry(entry)
			if err != nil {
				return fmt.Errorf("decrypting %s: %w", entry.Filename, err)
			}
			filePath := filepath.Join(v.Dir, entry.Filename)
			if err := crypto.EncryptFile(filePath, data, newPassphrase); err != nil {
				return fmt.Errorf("re-encrypting %s: %w", entry.Filename, err)
			}
			fmt.Printf("  Re-encrypted %d/%d: %s\n", i+1, len(v.Manifest.Entries), entry.Title)
		}

		// Re-encrypt manifest with new passphrase
		v.Passphrase = newPassphrase
		if err := vault.SaveManifest(filepath.Join(v.Dir, vault.ManifestFile), v.Manifest, newPassphrase); err != nil {
			return fmt.Errorf("re-encrypting manifest: %w", err)
		}

		fmt.Println("\nPassphrase changed successfully.")
		fmt.Println("IMPORTANT: Update your physical passphrase backup and any switch configuration.")
		return nil
	},
}
