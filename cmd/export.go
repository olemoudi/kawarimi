package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

	header, err := vault.LoadHeader(vaultDir)
	if err != nil {
		return nil, fmt.Errorf("loading vault header: %w", err)
	}

	// Check if sealed payload is in the vault directory (V4 mode)
	sealedPayloadPath := filepath.Join(vaultDir, vault.SealedPayloadFile)
	if _, err := os.Stat(sealedPayloadPath); err == nil {
		return exportWithDMSKey(vaultDir, header, sealedPayloadPath)
	}

	// V3 legacy: prompt for sealed payload from email
	return exportWithSealedPayloadLegacy(vaultDir, header)
}

// exportWithDMSKey handles the V4 flow: sealed payload in vault, DMS key from email.
func exportWithDMSKey(vaultDir string, header *vault.Header, sealedPayloadPath string) (*vault.Vault, error) {
	ciphertext, err := os.ReadFile(sealedPayloadPath)
	if err != nil {
		return nil, fmt.Errorf("reading sealed payload: %w", err)
	}

	reader := bufio.NewReader(os.Stdin)

	// Prompt for DMS key (from email)
	fmt.Fprintln(os.Stderr, "Paste the DMS KEY from the email (base64 string):")
	dmsKeyBase64, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("reading DMS key: %w", err)
	}
	dmsKeyBase64 = strings.TrimSpace(dmsKeyBase64)

	dmsKey, err := crypto.DecodeDMSKey(dmsKeyBase64)
	if err != nil {
		return nil, fmt.Errorf("invalid DMS key: %w", err)
	}
	defer crypto.ZeroBytes(dmsKey)

	// Prompt for recipient passphrase (from physical card)
	passphrase, err := crypto.PromptPassphrase("Enter recipient passphrase (from physical card): ")
	if err != nil {
		return nil, fmt.Errorf("reading passphrase: %w", err)
	}

	// Unseal with combined key (DMS key + passphrase)
	fmt.Fprintln(os.Stderr, "Decrypting sealed payload...")
	entropy, err := crypto.UnsealMnemonicV4(ciphertext, dmsKey, passphrase)
	if err != nil {
		return nil, fmt.Errorf("decrypting sealed payload (wrong DMS key or passphrase?): %w", err)
	}
	defer crypto.ZeroBytes(entropy)

	// Convert entropy to mnemonic words
	words, err := crypto.EncodeMnemonic(entropy)
	if err != nil {
		return nil, fmt.Errorf("encoding mnemonic: %w", err)
	}

	// Open vault with mnemonic
	_, ageIdentity, err := header.OpenWithMnemonic(words)
	if err != nil {
		return nil, fmt.Errorf("unlocking vault: %w", err)
	}

	fmt.Fprintln(os.Stderr, "Vault unlocked successfully.")
	return vault.OpenV2(vaultDir, ageIdentity, header.AgeRecipient)
}

// exportWithSealedPayloadLegacy handles V3: sealed payload from email + passphrase from card.
func exportWithSealedPayloadLegacy(vaultDir string, header *vault.Header) (*vault.Vault, error) {
	reader := bufio.NewReader(os.Stdin)

	// Prompt for sealed payload
	fmt.Fprintln(os.Stderr, "Paste the sealed payload from the email (base64 string):")
	sealedBase64, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("reading sealed payload: %w", err)
	}
	sealedBase64 = strings.TrimSpace(sealedBase64)

	ciphertext, err := crypto.DecodeSealedPayload(sealedBase64)
	if err != nil {
		return nil, fmt.Errorf("invalid sealed payload: %w", err)
	}

	// Prompt for recipient passphrase (echo suppressed to prevent shoulder surfing)
	passphrase, err := crypto.PromptPassphrase("Enter recipient passphrase (from physical card): ")
	if err != nil {
		return nil, fmt.Errorf("reading passphrase: %w", err)
	}

	// Unseal to recover mnemonic entropy
	fmt.Fprintln(os.Stderr, "Decrypting sealed payload...")
	entropy, err := crypto.UnsealMnemonic(ciphertext, passphrase)
	if err != nil {
		return nil, fmt.Errorf("decrypting sealed payload (wrong passphrase?): %w", err)
	}
	defer crypto.ZeroBytes(entropy)

	// Convert entropy to mnemonic words
	words, err := crypto.EncodeMnemonic(entropy)
	if err != nil {
		return nil, fmt.Errorf("encoding mnemonic: %w", err)
	}

	// Open vault with mnemonic
	_, ageIdentity, err := header.OpenWithMnemonic(words)
	if err != nil {
		return nil, fmt.Errorf("unlocking vault: %w", err)
	}

	fmt.Fprintln(os.Stderr, "Vault unlocked successfully.")
	return vault.OpenV2(vaultDir, ageIdentity, header.AgeRecipient)
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
