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
	rootCmd.AddCommand(repairCmd)
}

var repairCmd = &cobra.Command{
	Use:   "repair",
	Short: "Rebuild the vault index (manifest) from the encrypted entry files",
	Long: `Recovers a vault whose manifest.age is missing or corrupt by decrypting every
entry file with your identity and rebuilding the index. Use it if 'list' or 'show'
fail with a manifest error but the encrypted entry files are still present.

Titles/timestamps that lived only in the manifest are approximated from the file
names; every decryptable entry is re-indexed so the vault works again.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		header, err := vault.LoadHeader(cfg.VaultDir)
		if err != nil {
			return fmt.Errorf("loading vault header (repair needs a V2 vault): %w", err)
		}

		identity, err := unlockIdentity(cfg, header)
		if err != nil {
			return err
		}

		_, n, err := vault.RebuildManifest(cfg.VaultDir, identity, header.AgeRecipient)
		if err != nil {
			return fmt.Errorf("rebuilding manifest: %w", err)
		}

		fmt.Printf("Rebuilt the vault index with %d entr%s.\n", n, plural(n))
		fmt.Println("Run 'kawarimi list' to confirm, then re-check titles (they were")
		fmt.Println("reconstructed from file names and may need editing).")
		return nil
	},
}

func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

// unlockIdentity unlocks the vault to its age identity without loading the manifest
// (which may be the corrupt thing we are repairing). It uses this device's owner
// slot when a device key is present, else the 8-word mnemonic.
func unlockIdentity(cfg *config.Config, header *vault.Header) (string, error) {
	appDir, err := config.AppDirPath()
	if err != nil {
		return "", err
	}
	deviceKeyPath := filepath.Join(appDir, "device.key")

	if _, err := os.Stat(deviceKeyPath); err == nil {
		password, err := crypto.PromptPassphrase("Enter password: ")
		if err != nil {
			return "", err
		}
		dkf, err := crypto.LoadDeviceKeyFile(deviceKeyPath)
		if err != nil {
			return "", fmt.Errorf("loading device key: %w", err)
		}
		deviceKey, err := crypto.DecryptDeviceKey(dkf, password)
		if err != nil {
			return "", fmt.Errorf("decrypting device key (wrong password?): %w", err)
		}
		defer crypto.ZeroBytes(deviceKey)
		_, identity, err := header.OpenWithOwner(password, deviceKey)
		if err != nil {
			return "", fmt.Errorf("unlocking vault: %w", err)
		}
		return identity, nil
	}

	fmt.Fprint(os.Stderr, "No device key found. Enter 8 mnemonic words (space-separated): ")
	words := make([]string, 8)
	for i := range words {
		if _, err := fmt.Scan(&words[i]); err != nil {
			return "", fmt.Errorf("reading mnemonic word %d: %w", i+1, err)
		}
	}
	_, identity, err := header.OpenWithMnemonic(words)
	if err != nil {
		return "", fmt.Errorf("unlocking vault: %w", err)
	}
	return identity, nil
}
