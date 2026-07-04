package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/olemoudi/kawarimi/internal/config"
	"github.com/olemoudi/kawarimi/internal/crypto"
	"github.com/olemoudi/kawarimi/internal/setup"
	"github.com/olemoudi/kawarimi/internal/vault"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(initCmd)
}

var initCmd = &cobra.Command{
	Use:   "init [vault-directory]",
	Short: "Initialize a new vault",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("getting home directory: %w", err)
		}

		vaultDir := filepath.Join(home, "kawarimi-vault")
		if len(args) > 0 {
			vaultDir = args[0]
		}

		// Check if config already exists
		if cfg, err := config.Load(); err == nil {
			return fmt.Errorf("vault already configured at %s — delete %s/%s to reinitialize",
				cfg.VaultDir, filepath.Join(home, config.AppDir), config.ConfigFile)
		}

		fmt.Println("Set a password for daily vault access.")
		password, err := crypto.PromptPassphraseConfirm()
		if err != nil {
			return err
		}

		secrets, err := setup.InitVault(setup.InitOptions{
			VaultDir: vaultDir,
			Password: password,
		})
		if err != nil {
			return err
		}

		appDir, err := config.AppDirPath()
		if err != nil {
			return err
		}

		// Display critical secrets
		fmt.Println()
		fmt.Println("========================================")
		fmt.Println("  WRITE THESE DOWN AND STORE SAFELY")
		fmt.Println("  They will NOT be shown again!")
		fmt.Println("========================================")
		fmt.Println()
		fmt.Println("MNEMONIC WORDS (personal backup — store in a safe):")
		for i, w := range secrets.MnemonicWords {
			fmt.Printf("  %d. %s\n", i+1, w)
		}
		fmt.Println()
		fmt.Println("RECOVERY CODE (to regain access if you lose this device):")
		fmt.Printf("  %s\n", secrets.RecoveryCode)
		fmt.Println()
		fmt.Println("RECIPIENT PASSPHRASE (print on a card, give to your recipients):")
		fmt.Printf("  %s\n", secrets.RecipientPassphrase)
		fmt.Println()
		fmt.Println("  The mnemonic above is your personal backup.")
		fmt.Println("  The recipient passphrase is what your recipients need")
		fmt.Println("  (along with the DMS key from the dead man's switch email)")
		fmt.Println("  to decrypt the vault.")
		fmt.Println()
		fmt.Println("========================================")
		fmt.Println()
		fmt.Printf("Vault initialized at %s\n", secrets.VaultDir)
		fmt.Printf("Device key saved to %s\n", secrets.DeviceKeyPath)
		fmt.Printf("Sealed payload saved to %s\n", filepath.Join(vaultDir, vault.SealedPayloadFile))
		fmt.Printf("DMS key saved to %s\n", filepath.Join(appDir, "dms-key"))
		fmt.Printf("Config saved to ~/%s/%s\n", config.AppDir, config.ConfigFile)
		fmt.Println()
		fmt.Println("Next steps:")
		fmt.Println("  kawarimi add note \"Bank Accounts\"    — add a text note")
		fmt.Println("  kawarimi add credential               — add login credentials")
		fmt.Println("  kawarimi add document invoice.pdf      — add a document")
		fmt.Println("  kawarimi switch setup                  — configure the dead man's switch")
		fmt.Println("  kawarimi package build                 — build distributable vault package")

		return nil
	},
}
