package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/olemoudi/kawarimi/internal/config"
	"github.com/olemoudi/kawarimi/internal/vault"
	"github.com/spf13/cobra"
)

var useMnemonic bool

func init() {
	exportCmd.Flags().BoolVar(&useMnemonic, "mnemonic", false, "Use mnemonic words to decrypt (receiver mode)")
	rootCmd.AddCommand(exportCmd)
}

var exportCmd = &cobra.Command{
	Use:   "export <output-directory>",
	Short: "Decrypt entire vault to a directory",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		outputDir := args[0]

		var v *vault.Vault
		var err error

		if useMnemonic {
			v, err = exportWithMnemonic()
		} else {
			v, err = openVault()
		}
		if err != nil {
			return err
		}

		if err := v.Export(outputDir); err != nil {
			return err
		}

		fmt.Printf("Vault exported to %s\n", outputDir)
		fmt.Printf("%d entries decrypted\n", len(v.Manifest.Entries))
		return nil
	},
}

func exportWithMnemonic() (*vault.Vault, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	header, err := vault.LoadHeader(cfg.VaultDir)
	if err != nil {
		// No header — try detecting vault dir from args or current dir
		return nil, fmt.Errorf("loading vault header: %w", err)
	}

	fmt.Fprint(os.Stderr, "Enter 8 mnemonic words (space-separated): ")
	var words []string
	for i := 0; i < 8; i++ {
		var w string
		if _, err := fmt.Scan(&w); err != nil {
			return nil, fmt.Errorf("reading mnemonic word %d: %w", i+1, err)
		}
		words = append(words, w)
	}

	_, ageIdentity, err := header.OpenWithMnemonic(words)
	if err != nil {
		return nil, fmt.Errorf("unlocking vault with mnemonic: %w", err)
	}

	v, err := vault.OpenV2(cfg.VaultDir, ageIdentity, header.AgeRecipient)
	if err != nil {
		return nil, fmt.Errorf("opening vault: %w", err)
	}

	return v, nil
}

// exportStandalone opens a vault from a given directory (no config needed).
// This is for the receiver who just has the vault files + binary.
func exportStandalone(vaultDir string) (*vault.Vault, error) {
	headerPath := filepath.Join(vaultDir, vault.HeaderFile)
	if _, err := os.Stat(headerPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("no vault header found at %s", vaultDir)
	}

	header, err := vault.LoadHeader(vaultDir)
	if err != nil {
		return nil, err
	}

	fmt.Fprint(os.Stderr, "Enter 8 mnemonic words (space-separated): ")
	var words []string
	for i := 0; i < 8; i++ {
		var w string
		if _, err := fmt.Scan(&w); err != nil {
			return nil, fmt.Errorf("reading mnemonic word %d: %w", i+1, err)
		}
		words = append(words, w)
	}

	_, ageIdentity, err := header.OpenWithMnemonic(words)
	if err != nil {
		return nil, fmt.Errorf("unlocking vault: %w", err)
	}

	return vault.OpenV2(vaultDir, ageIdentity, header.AgeRecipient)
}
