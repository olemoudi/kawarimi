package cmd

import (
	"fmt"
	"os"

	"github.com/olemoudi/kawarimi/internal/config"
	"github.com/olemoudi/kawarimi/internal/crypto"
	"github.com/olemoudi/kawarimi/internal/vault"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "kawarimi",
	Short: "Encrypted end-of-life information vault",
	Long:  "Kawarimi manages an encrypted vault of instructions, credentials, and documents for your family.",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// openVault is a helper that loads config, prompts for passphrase, and opens the vault.
func openVault() (*vault.Vault, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	passphrase, err := crypto.PromptPassphrase("Enter passphrase: ")
	if err != nil {
		return nil, err
	}

	v, err := vault.Open(cfg.VaultDir, passphrase)
	if err != nil {
		return nil, fmt.Errorf("opening vault: %w", err)
	}

	return v, nil
}
