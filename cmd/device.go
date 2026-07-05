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

func init() {
	deviceCmd.AddCommand(deviceAddCmd)
	deviceCmd.AddCommand(deviceEnrollCmd)
	deviceCmd.AddCommand(deviceAcceptCmd)
	rootCmd.AddCommand(deviceCmd)
}

var deviceCmd = &cobra.Command{
	Use:   "device",
	Short: "Manage device keys for vault access",
}

var deviceAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Register this device using recovery code",
	Long:  "Creates a new device key on this machine using password + recovery code.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		appDir, err := config.AppDirPath()
		if err != nil {
			return err
		}
		deviceKeyPath := filepath.Join(appDir, "device.key")
		if _, err := os.Stat(deviceKeyPath); err == nil {
			return fmt.Errorf("device key already exists at %s — this device is already registered", deviceKeyPath)
		}

		header, err := vault.LoadHeader(cfg.VaultDir)
		if err != nil {
			return fmt.Errorf("loading vault header: %w", err)
		}

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

		return enrollNewDevice(cfg, header, masterKey, password)
	},
}

var deviceEnrollCmd = &cobra.Command{
	Use:   "enroll",
	Short: "Generate an enrollment token for a new device",
	Long:  "Unlocks the vault on this (trusted) device and generates a code-protected token for enrolling another device.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		header, err := vault.LoadHeader(cfg.VaultDir)
		if err != nil {
			return fmt.Errorf("loading vault header: %w", err)
		}

		// Unlock vault on this device
		appDir, err := config.AppDirPath()
		if err != nil {
			return err
		}
		deviceKeyPath := filepath.Join(appDir, "device.key")

		password, err := crypto.PromptPassphrase("Enter password: ")
		if err != nil {
			return err
		}

		dkf, err := crypto.LoadDeviceKeyFile(deviceKeyPath)
		if err != nil {
			return fmt.Errorf("loading device key: %w", err)
		}

		deviceKey, err := crypto.DecryptDeviceKey(dkf, password)
		if err != nil {
			return fmt.Errorf("wrong password: %w", err)
		}
		defer crypto.ZeroBytes(deviceKey)

		masterKey, _, err := header.OpenWithOwner(password, deviceKey)
		if err != nil {
			return fmt.Errorf("unlocking vault: %w", err)
		}
		defer crypto.ZeroBytes(masterKey)

		// Generate enrollment token
		tokenStr, code, err := vault.GenerateEnrollmentToken(masterKey)
		if err != nil {
			return fmt.Errorf("generating token: %w", err)
		}

		fmt.Println()
		fmt.Println("========================================")
		fmt.Println("  ENROLLMENT TOKEN")
		fmt.Println("========================================")
		fmt.Println()
		fmt.Println("On the new device, run:")
		fmt.Println("  kawarimi device accept")
		fmt.Println()
		fmt.Println("Token (paste this on the new device):")
		fmt.Println(tokenStr)
		fmt.Println()
		fmt.Printf("Code (4 words, tell the new device out-of-band): %s\n", code)
		fmt.Println()
		fmt.Println("The token expires in 10 minutes.")
		fmt.Println("========================================")

		return nil
	},
}

var deviceAcceptCmd = &cobra.Command{
	Use:   "accept",
	Short: "Accept an enrollment token from a trusted device",
	Long:  "Uses an enrollment token + code to register this device. The new device can use its own password.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		appDir, err := config.AppDirPath()
		if err != nil {
			return err
		}
		deviceKeyPath := filepath.Join(appDir, "device.key")
		if _, err := os.Stat(deviceKeyPath); err == nil {
			return fmt.Errorf("device key already exists at %s — this device is already registered", deviceKeyPath)
		}

		// Get token and code (the code is 4 words, so read whole lines)
		reader := bufio.NewReader(os.Stdin)
		fmt.Print("Paste enrollment token: ")
		tokenLine, _ := reader.ReadString('\n')
		tokenStr := strings.TrimSpace(tokenLine)

		fmt.Print("Enter the 4-word code: ")
		codeLine, _ := reader.ReadString('\n')
		code := strings.TrimSpace(codeLine)

		// Decrypt token
		masterKey, err := vault.AcceptEnrollmentToken(tokenStr, code)
		if err != nil {
			return fmt.Errorf("token rejected: %w", err)
		}
		defer crypto.ZeroBytes(masterKey)

		// Set password for this device
		fmt.Println("\nSet a password for this device (can be different from other devices).")
		password, err := crypto.PromptNewPassphrase()
		if err != nil {
			return err
		}

		header, err := vault.LoadHeader(cfg.VaultDir)
		if err != nil {
			return fmt.Errorf("loading vault header: %w", err)
		}

		return enrollNewDevice(cfg, header, masterKey, password)
	},
}

// enrollNewDevice generates a device key, adds an owner slot, and saves everything.
func enrollNewDevice(cfg *config.Config, header *vault.Header, masterKey []byte, password string) error {
	appDir, err := config.AppDirPath()
	if err != nil {
		return err
	}

	deviceKey, err := crypto.GenerateDeviceKey()
	if err != nil {
		return err
	}
	defer crypto.ZeroBytes(deviceKey)

	hostname, err := os.Hostname()
	if err != nil {
		hostname = "default"
	}

	if err := header.AddOwnerSlot(password, deviceKey, hostname, masterKey, nil); err != nil {
		return fmt.Errorf("adding owner slot: %w", err)
	}

	if err := vault.SaveHeader(cfg.VaultDir, header); err != nil {
		return fmt.Errorf("saving header: %w", err)
	}

	if err := os.MkdirAll(appDir, 0700); err != nil {
		return fmt.Errorf("creating app directory: %w", err)
	}

	dkf, err := crypto.EncryptDeviceKey(deviceKey, password)
	if err != nil {
		return fmt.Errorf("encrypting device key: %w", err)
	}

	deviceKeyPath := filepath.Join(appDir, "device.key")
	if err := crypto.SaveDeviceKeyFile(deviceKeyPath, dkf); err != nil {
		return fmt.Errorf("saving device key: %w", err)
	}

	fmt.Printf("Device registered as %q\n", hostname)
	fmt.Printf("Device key saved to %s\n", deviceKeyPath)
	fmt.Println("You can now use 'kawarimi' commands with your password.")
	return nil
}
