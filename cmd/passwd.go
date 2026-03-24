package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/olemoudi/kawarimi/internal/config"
	"github.com/olemoudi/kawarimi/internal/crypto"
	"github.com/olemoudi/kawarimi/internal/deadswitch"
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

		// Re-encrypt manifest to a temporary too
		manifestRekeyPath := filepath.Join(v.Dir, vault.ManifestFile+".rekey")
		if err := vault.SaveManifest(manifestRekeyPath, v.Manifest, newPassphrase); err != nil {
			cleanupRekeyFiles(rekeyPaths)
			os.Remove(manifestRekeyPath)
			return fmt.Errorf("re-encrypting manifest: %w", err)
		}

		// Phase 2: All succeeded — atomically rename .rekey files to final paths
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

		v.Passphrase = newPassphrase

		// Auto-update switch payload if configured
		appDir, err := config.AppDirPath()
		if err == nil && deadswitch.IsSwitchConfigured(appDir) {
			if err := deadswitch.StoreSwitchPayload(appDir, newPassphrase); err != nil {
				fmt.Fprintf(os.Stderr, "WARNING: Failed to update switch payload: %v\n", err)
				fmt.Fprintln(os.Stderr, "Run 'kawarimi switch setup' to reconfigure the switch.")
			} else {
				fmt.Println("Switch payload updated with new passphrase.")
			}
		}

		fmt.Println("\nPassphrase changed successfully.")
		fmt.Println("IMPORTANT: Update your physical passphrase backup.")
		return nil
	},
}

func cleanupRekeyFiles(paths []string) {
	for _, p := range paths {
		os.Remove(p)
	}
}
