package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/olemoudi/kawarimi/internal/config"
	"github.com/olemoudi/kawarimi/internal/crypto"
	"github.com/olemoudi/kawarimi/internal/vault"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(migrateCmd)
}

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate vault from v1 (passphrase) to v2 (multi-slot encryption)",
	Long:  "Re-encrypts all vault files from scrypt-based passphrase encryption to the new X25519 identity-based system with multiple access slots.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		// Check if already v2
		if vault.IsV2Vault(cfg.VaultDir) {
			return fmt.Errorf("vault is already v2 (vault_header.json exists)")
		}

		fmt.Println("This will migrate your vault to v2 with multi-slot encryption.")
		fmt.Println("All files will be re-encrypted. This may take a while for large vaults.")
		fmt.Println()

		// Get current passphrase
		passphrase, err := crypto.PromptPassphrase("Enter current passphrase: ")
		if err != nil {
			return err
		}

		// Get new password
		fmt.Println("\nSet a password for v2 vault access.")
		password, err := crypto.PromptPassphraseConfirm()
		if err != nil {
			return err
		}

		hostname, err := os.Hostname()
		if err != nil {
			hostname = "default"
		}

		fmt.Println("\nMigrating vault...")
		result, err := vault.MigrateV1ToV2(cfg.VaultDir, passphrase, password, hostname)
		if err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
		defer crypto.ZeroBytes(result.MasterKey)

		// Save device key
		appDir, err := config.AppDirPath()
		if err != nil {
			return err
		}

		dkf, err := crypto.EncryptDeviceKey(result.DeviceKey, password)
		if err != nil {
			return fmt.Errorf("encrypting device key: %w", err)
		}
		deviceKeyPath := filepath.Join(appDir, "device.key")
		if err := crypto.SaveDeviceKeyFile(deviceKeyPath, dkf); err != nil {
			return fmt.Errorf("saving device key: %w", err)
		}

		// Display secrets
		fmt.Println()
		fmt.Println("========================================")
		fmt.Println("  WRITE THESE DOWN AND STORE SAFELY")
		fmt.Println("  They will NOT be shown again!")
		fmt.Println("========================================")
		fmt.Println()
		fmt.Println("MNEMONIC WORDS (for your family/receiver):")
		for i, w := range result.MnemonicWords {
			fmt.Printf("  %d. %s\n", i+1, w)
		}
		fmt.Println()
		fmt.Println("RECOVERY CODE (to regain access if you lose this device):")
		fmt.Printf("  %s\n", crypto.FormatRecoveryCode(result.RecoveryCode))
		fmt.Println()
		fmt.Println("========================================")
		fmt.Println()
		fmt.Println("Migration complete!")
		fmt.Printf("Device key saved to %s\n", deviceKeyPath)
		fmt.Println("Your old passphrase is no longer needed for vault access.")

		crypto.ZeroBytes(result.DeviceKey)
		crypto.ZeroBytes(result.RecoveryCode)

		return nil
	},
}
