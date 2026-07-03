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

		// Get hostname for device ID
		hostname, err := os.Hostname()
		if err != nil {
			hostname = "default"
		}

		fmt.Println("Set a password for daily vault access.")
		password, err := crypto.PromptPassphraseConfirm()
		if err != nil {
			return err
		}

		// Create header with all slots
		result, err := vault.NewHeader(vault.InitParams{
			Password: password,
			DeviceID: hostname,
		})
		if err != nil {
			return fmt.Errorf("creating vault header: %w", err)
		}
		defer crypto.ZeroBytes(result.MasterKey)

		// Save header to vault dir (create dir first)
		if err := os.MkdirAll(vaultDir, 0700); err != nil {
			return fmt.Errorf("creating vault directory: %w", err)
		}
		if err := vault.SaveHeader(vaultDir, result.Header); err != nil {
			return fmt.Errorf("saving vault header: %w", err)
		}

		// Create vault with identity-based encryption
		v, err := vault.CreateV2(vaultDir, result.AgeIdentity, result.Header.AgeRecipient)
		if err != nil {
			return fmt.Errorf("creating vault: %w", err)
		}

		// Save encrypted device key
		appDir, err := config.AppDirPath()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(appDir, 0700); err != nil {
			return fmt.Errorf("creating app directory: %w", err)
		}

		dkf, err := crypto.EncryptDeviceKey(result.DeviceKey, password)
		if err != nil {
			return fmt.Errorf("encrypting device key: %w", err)
		}
		deviceKeyPath := filepath.Join(appDir, "device.key")
		if err := crypto.SaveDeviceKeyFile(deviceKeyPath, dkf); err != nil {
			return fmt.Errorf("saving device key: %w", err)
		}

		// Save config
		cfg := config.DefaultConfig(v.Dir)
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}

		// Generate the recipient passphrase and seal the mnemonic under it plus a
		// fresh DMS key (V4 key-split architecture).
		recipientPassphrase, err := crypto.GenerateRecipientPassphrase()
		if err != nil {
			return fmt.Errorf("generating recipient passphrase: %w", err)
		}

		mnemonicEntropy, err := crypto.DecodeMnemonic(result.MnemonicWords)
		if err != nil {
			return fmt.Errorf("encoding mnemonic entropy: %w", err)
		}

		_, err = sealAndInstallV4(vaultDir, appDir, mnemonicEntropy, recipientPassphrase)
		crypto.ZeroBytes(mnemonicEntropy)
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
		for i, w := range result.MnemonicWords {
			fmt.Printf("  %d. %s\n", i+1, w)
		}
		fmt.Println()
		fmt.Println("RECOVERY CODE (to regain access if you lose this device):")
		fmt.Printf("  %s\n", crypto.FormatRecoveryCode(result.RecoveryCode))
		fmt.Println()
		fmt.Println("RECIPIENT PASSPHRASE (print on a card, give to your recipients):")
		fmt.Printf("  %s\n", recipientPassphrase)
		fmt.Println()
		fmt.Println("  The mnemonic above is your personal backup.")
		fmt.Println("  The recipient passphrase is what your recipients need")
		fmt.Println("  (along with the DMS key from the dead man's switch email)")
		fmt.Println("  to decrypt the vault.")
		fmt.Println()
		fmt.Println("========================================")
		fmt.Println()
		fmt.Printf("Vault initialized at %s\n", v.Dir)
		fmt.Printf("Device key saved to %s\n", deviceKeyPath)
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

		// Zero sensitive data
		crypto.ZeroBytes(result.DeviceKey)
		crypto.ZeroBytes(result.RecoveryCode)

		return nil
	},
}

// sealAndInstallV4 seals the mnemonic entropy under a fresh DMS key plus the given
// recipient passphrase (V4 key-split), writes the sealed payload into the vault, and
// stores the DMS key locally. Returns the DMS key (base64) for display. Shared by
// `init` and `switch rekey` so the two cannot drift.
func sealAndInstallV4(vaultDir, appDir string, entropy []byte, recipientPassphrase string) (dmsKeyB64 string, err error) {
	dmsKey, err := crypto.GenerateDMSKey()
	if err != nil {
		return "", fmt.Errorf("generating DMS key: %w", err)
	}
	defer crypto.ZeroBytes(dmsKey)

	sealedPayload, err := crypto.SealMnemonicV4(entropy, dmsKey, recipientPassphrase)
	if err != nil {
		return "", fmt.Errorf("sealing mnemonic: %w", err)
	}

	// The sealed payload lives in the vault dir (publicly distributed in the package).
	sealedPath := filepath.Join(vaultDir, vault.SealedPayloadFile)
	if err := os.WriteFile(sealedPath, sealedPayload, 0600); err != nil {
		return "", fmt.Errorf("saving sealed payload: %w", err)
	}

	// The DMS key is kept locally for `switch seed` to publish as the GitHub secret.
	dmsKeyPath := filepath.Join(appDir, "dms-key")
	if err := os.WriteFile(dmsKeyPath, []byte(crypto.EncodeDMSKey(dmsKey)), 0600); err != nil {
		return "", fmt.Errorf("saving DMS key: %w", err)
	}

	return crypto.EncodeDMSKey(dmsKey), nil
}
