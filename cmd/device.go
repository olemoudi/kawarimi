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
	deviceCmd.AddCommand(deviceAddCmd)
	rootCmd.AddCommand(deviceCmd)
}

var deviceCmd = &cobra.Command{
	Use:   "device",
	Short: "Manage device keys for vault access",
}

var deviceAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Register this device for vault access",
	Long:  "Creates a new device key on this machine and adds an owner slot to the vault. Requires password + recovery code.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		// Check if device key already exists
		appDir, err := config.AppDirPath()
		if err != nil {
			return err
		}
		deviceKeyPath := filepath.Join(appDir, "device.key")
		if _, err := os.Stat(deviceKeyPath); err == nil {
			return fmt.Errorf("device key already exists at %s — this device is already registered", deviceKeyPath)
		}

		// Load header
		header, err := vault.LoadHeader(cfg.VaultDir)
		if err != nil {
			return fmt.Errorf("loading vault header: %w", err)
		}

		// Authenticate with recovery code
		password, err := crypto.PromptPassphrase("Enter password: ")
		if err != nil {
			return err
		}

		fmt.Print("Enter recovery code: ")
		var codeStr string
		fmt.Scanln(&codeStr)

		recoveryCode, err := crypto.DecodeRecoveryCode(codeStr)
		if err != nil {
			return fmt.Errorf("invalid recovery code: %w", err)
		}

		masterKey, _, err := header.OpenWithRecovery(password, recoveryCode)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		defer crypto.ZeroBytes(masterKey)

		// Generate new device key
		deviceKey, err := crypto.GenerateDeviceKey()
		if err != nil {
			return err
		}
		defer crypto.ZeroBytes(deviceKey)

		// Get hostname
		hostname, err := os.Hostname()
		if err != nil {
			hostname = "default"
		}

		// Add owner slot
		if err := header.AddOwnerSlot(password, deviceKey, hostname, masterKey, nil); err != nil {
			return fmt.Errorf("adding owner slot: %w", err)
		}

		// Save updated header
		if err := vault.SaveHeader(cfg.VaultDir, header); err != nil {
			return fmt.Errorf("saving header: %w", err)
		}

		// Encrypt and save device key
		if err := os.MkdirAll(appDir, 0700); err != nil {
			return fmt.Errorf("creating app directory: %w", err)
		}

		dkf, err := crypto.EncryptDeviceKey(deviceKey, password)
		if err != nil {
			return fmt.Errorf("encrypting device key: %w", err)
		}
		if err := crypto.SaveDeviceKeyFile(deviceKeyPath, dkf); err != nil {
			return fmt.Errorf("saving device key: %w", err)
		}

		fmt.Printf("Device registered as %q\n", hostname)
		fmt.Printf("Device key saved to %s\n", deviceKeyPath)
		fmt.Println("You can now use 'kawarimi' commands with just your password.")
		return nil
	},
}
