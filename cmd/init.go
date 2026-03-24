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

		passphrase, err := crypto.PromptPassphraseConfirm()
		if err != nil {
			return err
		}

		v, err := vault.Create(vaultDir, passphrase)
		if err != nil {
			return err
		}

		cfg := config.DefaultConfig(v.Dir)
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}

		fmt.Printf("Vault initialized at %s\n", v.Dir)
		fmt.Printf("Config saved to ~/%s/%s\n", config.AppDir, config.ConfigFile)
		fmt.Println("\nNext steps:")
		fmt.Println("  kawarimi add note \"Bank Accounts\"    — add a text note")
		fmt.Println("  kawarimi add credential               — add login credentials")
		fmt.Println("  kawarimi add document invoice.pdf      — add a document")
		fmt.Println("\nIMPORTANT: Write down your passphrase and store it in a safe place.")
		fmt.Println("Your family will need it to decrypt the vault.")

		return nil
	},
}
