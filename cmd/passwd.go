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
	rootCmd.AddCommand(passwdCmd)
}

var passwdCmd = &cobra.Command{
	Use:   "passwd",
	Short: "Change the vault password",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		// Check if this is a v2 vault
		headerPath := filepath.Join(cfg.VaultDir, vault.HeaderFile)
		if _, err := os.Stat(headerPath); err == nil {
			return passwdV2(cfg)
		}

		// Fall back to v1 password change
		return passwdV1(cfg)
	},
}

func passwdV2(cfg *config.Config) error {
	header, err := vault.LoadHeader(cfg.VaultDir)
	if err != nil {
		return err
	}

	appDir, err := config.AppDirPath()
	if err != nil {
		return err
	}

	// Prompt for current password and open vault
	currentPassword, err := crypto.PromptPassphrase("Enter current password: ")
	if err != nil {
		return err
	}

	deviceKeyPath := filepath.Join(appDir, "device.key")
	dkf, err := crypto.LoadDeviceKeyFile(deviceKeyPath)
	if err != nil {
		return fmt.Errorf("loading device key: %w", err)
	}

	deviceKey, err := crypto.DecryptDeviceKey(dkf, currentPassword)
	if err != nil {
		return fmt.Errorf("wrong current password: %w", err)
	}
	defer crypto.ZeroBytes(deviceKey)

	masterKey, _, err := header.OpenWithOwner(currentPassword, deviceKey)
	if err != nil {
		return fmt.Errorf("unlocking vault: %w", err)
	}
	defer crypto.ZeroBytes(masterKey)

	// Prompt for new password
	fmt.Println("Set a new password.")
	newPassword, err := crypto.PromptPassphraseConfirm()
	if err != nil {
		return err
	}

	// Get hostname for device ID
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "default"
	}

	// Update owner slot with new password
	if err := header.UpdateOwnerSlot(hostname, newPassword, deviceKey, masterKey); err != nil {
		return fmt.Errorf("updating owner slot: %w", err)
	}

	// Decrypt recovery code and update recovery slot
	encRC, rcNonce, err := header.GetEncryptedRecoveryCode()
	if err != nil {
		return fmt.Errorf("getting recovery code: %w", err)
	}

	recoveryCode, err := crypto.UnwrapKey(masterKey, encRC, rcNonce)
	if err != nil {
		return fmt.Errorf("decrypting recovery code: %w", err)
	}
	defer crypto.ZeroBytes(recoveryCode)

	if err := header.UpdateRecoverySlot(newPassword, recoveryCode, masterKey); err != nil {
		return fmt.Errorf("updating recovery slot: %w", err)
	}

	// Save updated header
	if err := vault.SaveHeader(cfg.VaultDir, header); err != nil {
		return fmt.Errorf("saving header: %w", err)
	}

	// Re-encrypt device key with new password
	newDKF, err := crypto.EncryptDeviceKey(deviceKey, newPassword)
	if err != nil {
		return fmt.Errorf("re-encrypting device key: %w", err)
	}
	if err := crypto.SaveDeviceKeyFile(deviceKeyPath, newDKF); err != nil {
		return fmt.Errorf("saving device key: %w", err)
	}

	fmt.Println("\nPassword changed successfully.")
	fmt.Println("Data files were NOT re-encrypted (O(1) operation).")
	fmt.Println("Mnemonic words remain unchanged.")
	return nil
}

func passwdV1(cfg *config.Config) error {
	v, err := openVaultV1(cfg)
	if err != nil {
		return err
	}

	fmt.Println("Set a new passphrase for the vault.")
	newPassphrase, err := crypto.PromptPassphraseConfirm()
	if err != nil {
		return err
	}

	// Phase 1: Write all re-encrypted files to .rekey temporaries
	var rekeyPaths []string
	for i, entry := range v.Manifest.Entries {
		data, err := v.ShowEntry(entry)
		if err != nil {
			cleanupRekeyFiles(rekeyPaths)
			return fmt.Errorf("decrypting %s: %w", entry.Filename, err)
		}
		rekeyPath := filepath.Join(v.Dir, entry.Filename+".rekey")
		if err := crypto.EncryptFile(rekeyPath, data, newPassphrase); err != nil {
			cleanupRekeyFiles(rekeyPaths)
			return fmt.Errorf("re-encrypting %s: %w", entry.Filename, err)
		}
		rekeyPaths = append(rekeyPaths, rekeyPath)
		fmt.Printf("  Re-encrypted %d/%d: %s\n", i+1, len(v.Manifest.Entries), entry.Title)
	}

	// Re-encrypt manifest
	manifestRekeyPath := filepath.Join(v.Dir, vault.ManifestFile+".rekey")
	if err := vault.SaveManifest(manifestRekeyPath, v.Manifest, newPassphrase); err != nil {
		cleanupRekeyFiles(rekeyPaths)
		os.Remove(manifestRekeyPath)
		return fmt.Errorf("re-encrypting manifest: %w", err)
	}

	// Phase 2: Atomic rename
	for i, entry := range v.Manifest.Entries {
		finalPath := filepath.Join(v.Dir, entry.Filename)
		if err := os.Rename(rekeyPaths[i], finalPath); err != nil {
			return fmt.Errorf("renaming %s: %w (vault may be in inconsistent state)", entry.Filename, err)
		}
	}
	manifestPath := filepath.Join(v.Dir, vault.ManifestFile)
	if err := os.Rename(manifestRekeyPath, manifestPath); err != nil {
		return fmt.Errorf("renaming manifest: %w", err)
	}

	fmt.Println("\nPassphrase changed successfully.")
	fmt.Println("IMPORTANT: Update your physical passphrase backup.")
	return nil
}

func cleanupRekeyFiles(paths []string) {
	for _, p := range paths {
		os.Remove(p)
	}
}
