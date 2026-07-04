package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"

	"github.com/olemoudi/kawarimi/internal/config"
	"github.com/olemoudi/kawarimi/internal/crypto"
	"github.com/olemoudi/kawarimi/internal/vault"
	"github.com/spf13/cobra"
)

var (
	useMnemonic bool
	useSealed   bool
	vaultPath   string
)

func init() {
	exportCmd.Flags().BoolVar(&useMnemonic, "mnemonic", false, "Use mnemonic words to decrypt (receiver mode)")
	exportCmd.Flags().BoolVar(&useSealed, "sealed", false, "Use sealed payload + recipient passphrase to decrypt (receiver mode)")
	exportCmd.Flags().StringVar(&vaultPath, "vault", "", "Path to vault directory (for standalone use without config)")
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

		if useSealed {
			v, err = exportWithSealedPayload()
		} else if useMnemonic {
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

func exportWithSealedPayload() (*vault.Vault, error) {
	// Determine vault directory
	vaultDir, err := resolveVaultDir()
	if err != nil {
		return nil, err
	}

	// V4: the sealed payload ships inside the vault directory. (Pre-V4 packages,
	// which emailed the sealed payload instead, were never publicly released and
	// are no longer supported.)
	sealedPayloadPath := filepath.Join(vaultDir, vault.SealedPayloadFile)
	if _, err := os.Stat(sealedPayloadPath); err != nil {
		return nil, fmt.Errorf("no sealed payload in %s — this looks like an old or incomplete package; download the newest package and try again", vaultDir)
	}
	return exportWithDMSKey(vaultDir)
}

// exportWithDMSKey handles the V4 flow: sealed payload in vault, DMS key from email.
func exportWithDMSKey(vaultDir string) (*vault.Vault, error) {
	reader := bufio.NewReader(os.Stdin)

	// Prompt for DMS key (from email)
	fmt.Fprintln(os.Stderr, "Paste the DMS KEY from the email (base64 string):")
	dmsKeyBase64, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("reading DMS key: %w", err)
	}
	dmsKey, err := crypto.DecodeDMSKeyLenient(dmsKeyBase64)
	if err != nil {
		return nil, fmt.Errorf("invalid DMS key: %w", err)
	}
	defer crypto.ZeroBytes(dmsKey)

	// Prompt for recipient passphrase (from physical card)
	passphrase, err := crypto.PromptPassphrase("Enter recipient passphrase (from physical card): ")
	if err != nil {
		return nil, fmt.Errorf("reading passphrase: %w", err)
	}

	fmt.Fprintln(os.Stderr, "Decrypting sealed payload...")
	v, err := vault.OpenSealedV4(vaultDir, dmsKey, passphrase)
	if err != nil {
		return nil, fmt.Errorf("could not decrypt: check the KEY and the card words — or this may be an old package copy, so download the newest one: %w", err)
	}
	fmt.Fprintln(os.Stderr, "Vault unlocked successfully.")
	return v, nil
}

func resolveVaultDir() (string, error) {
	// Explicit --vault flag
	if vaultPath != "" {
		return vaultPath, nil
	}

	// Try to auto-detect vault/ subdirectory (from extracted package)
	if _, err := os.Stat(filepath.Join("vault", vault.HeaderFile)); err == nil {
		return "vault", nil
	}

	// Fall back to config
	cfg, err := config.Load()
	if err != nil {
		return "", fmt.Errorf("no vault found: use --vault flag or run from extracted package directory: %w", err)
	}
	return cfg.VaultDir, nil
}

func exportWithMnemonic() (*vault.Vault, error) {
	vaultDir, err := resolveVaultDir()
	if err != nil {
		return nil, err
	}

	header, err := vault.LoadHeader(vaultDir)
	if err != nil {
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

	v, err := vault.OpenV2(vaultDir, ageIdentity, header.AgeRecipient)
	if err != nil {
		return nil, fmt.Errorf("opening vault: %w", err)
	}

	return v, nil
}
