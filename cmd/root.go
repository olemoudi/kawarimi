package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mattn/go-isatty"
	"github.com/olemoudi/kawarimi/internal/config"
	"github.com/olemoudi/kawarimi/internal/crypto"
	"github.com/olemoudi/kawarimi/internal/gui"
	"github.com/olemoudi/kawarimi/internal/recipient"
	"github.com/olemoudi/kawarimi/internal/vault"
	"github.com/spf13/cobra"
)

// version is overridden at build time via
// -ldflags "-X github.com/olemoudi/kawarimi/cmd.version=<v>".
var version = "dev"

var rootCmd = &cobra.Command{
	Use:     "kawarimi",
	Short:   "Encrypted end-of-life information vault",
	Long:    "Kawarimi manages an encrypted vault of instructions, credentials, and documents for your family.",
	Version: version,
	// With no subcommand: launch the recipient wizard when this looks like a
	// recipient's machine (a package is nearby, no owner device key, interactive);
	// launch the browser setup wizard on a fresh interactive machine (nothing
	// configured at all — the double-clicked-download case); otherwise print help.
	RunE: func(cmd *cobra.Command, args []string) error {
		if recipientContext() {
			cmd.SilenceUsage = true  // the wizard prints its own guidance
			cmd.SilenceErrors = true // (only affects this root invocation)
			return recipient.Run(recipient.Options{})
		}
		if ownerFirstRunContext() {
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			fmt.Println("No vault here yet — starting the setup wizard in your browser…")
			fmt.Println("Aún no hay caja fuerte — abriendo el asistente en tu navegador…")
			fmt.Println("(kawarimi --help)")
			fmt.Println()
			return gui.Run(gui.Options{Version: version})
		}
		return cmd.Help()
	},
}

// recipientContext reports whether bare `kawarimi` should launch the recipient
// wizard: an interactive session, no owner device key on this machine, and a sealed
// payload reachable nearby. All three guards must hold so owner machines, scripts,
// and CI are unaffected.
func recipientContext() bool {
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return false
	}
	if ownerDeviceKeyExists() {
		return false
	}
	cwd, _ := os.Getwd()
	exeDir := ""
	if exe, err := os.Executable(); err == nil {
		exeDir = filepath.Dir(exe)
	}
	return hasNearbySealedPayload(cwd, exeDir)
}

// ownerFirstRunContext reports whether bare `kawarimi` should launch the browser
// setup wizard: an interactive session on a machine with NOTHING configured — no
// config, no device key, and no recipient package nearby. This is the person who
// just downloaded the binary and double-clicked it; configured machines, scripts,
// and recipients are unaffected (recipientContext is checked first).
func ownerFirstRunContext() bool {
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return false
	}
	return firstRunDecision(configExists(), ownerDeviceKeyExists(), nearbyPayloadExists())
}

// firstRunDecision is the pure decision, split out for testing.
func firstRunDecision(hasConfig, hasDeviceKey, hasNearbyPayload bool) bool {
	return !hasConfig && !hasDeviceKey && !hasNearbyPayload
}

func configExists() bool {
	_, err := config.Load()
	return err == nil
}

func nearbyPayloadExists() bool {
	cwd, _ := os.Getwd()
	exeDir := ""
	if exe, err := os.Executable(); err == nil {
		exeDir = filepath.Dir(exe)
	}
	return hasNearbySealedPayload(cwd, exeDir)
}

func ownerDeviceKeyExists() bool {
	appDir, err := config.AppDirPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(appDir, "device.key"))
	return err == nil
}

func hasNearbySealedPayload(cwd, exeDir string) bool {
	for _, base := range []string{cwd, exeDir} {
		if base == "" {
			continue
		}
		for _, rel := range []string{
			filepath.Join(vault.PackageVaultDir, vault.SealedPayloadFile),
			vault.SealedPayloadFile,
		} {
			if _, err := os.Stat(filepath.Join(base, rel)); err == nil {
				return true
			}
		}
	}
	return false
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// openVault is a helper that loads config, detects vault version, prompts for credentials, and opens the vault.
func openVault() (*vault.Vault, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	// Check if this is a v2 vault (has vault_header.json)
	headerPath := filepath.Join(cfg.VaultDir, vault.HeaderFile)
	if _, err := os.Stat(headerPath); err == nil {
		return openVaultV2(cfg)
	}

	// Fall back to v1 passphrase-based vault
	return openVaultV1(cfg)
}

// openVaultV1 opens a legacy vault with a single passphrase.
func openVaultV1(cfg *config.Config) (*vault.Vault, error) {
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

// openVaultV2 opens a v2 vault using password + device key.
func openVaultV2(cfg *config.Config) (*vault.Vault, error) {
	header, err := vault.LoadHeader(cfg.VaultDir)
	if err != nil {
		return nil, fmt.Errorf("loading vault header: %w", err)
	}

	// Try to load device key
	appDir, err := config.AppDirPath()
	if err != nil {
		return nil, err
	}
	deviceKeyPath := filepath.Join(appDir, "device.key")

	if _, err := os.Stat(deviceKeyPath); err == nil {
		// Device key exists — use owner slot
		return openWithDeviceKey(cfg, header, deviceKeyPath)
	}

	// No device key — prompt for which access method to use
	fmt.Fprintln(os.Stderr, "No device key found. Use recovery code or mnemonic to access the vault.")
	fmt.Fprintln(os.Stderr, "  1) Recovery code (password + recovery code)")
	fmt.Fprintln(os.Stderr, "  2) Mnemonic words (receiver access)")
	fmt.Fprint(os.Stderr, "Choice [1]: ")

	var choice string
	fmt.Scanln(&choice)

	switch choice {
	case "2":
		return openWithMnemonic(cfg, header)
	default:
		return openWithRecovery(cfg, header)
	}
}

func openWithDeviceKey(cfg *config.Config, header *vault.Header, deviceKeyPath string) (*vault.Vault, error) {
	password, err := crypto.PromptPassphrase("Enter password: ")
	if err != nil {
		return nil, err
	}

	dkf, err := crypto.LoadDeviceKeyFile(deviceKeyPath)
	if err != nil {
		return nil, fmt.Errorf("loading device key: %w", err)
	}

	deviceKey, err := crypto.DecryptDeviceKey(dkf, password)
	if err != nil {
		return nil, fmt.Errorf("decrypting device key (wrong password?): %w", err)
	}
	defer crypto.ZeroBytes(deviceKey)

	_, ageIdentity, err := header.OpenWithOwner(password, deviceKey)
	if err != nil {
		return nil, fmt.Errorf("unlocking vault: %w", err)
	}

	v, err := vault.OpenV2(cfg.VaultDir, ageIdentity, header.AgeRecipient)
	if err != nil {
		return nil, fmt.Errorf("opening vault: %w", err)
	}

	return v, nil
}

func openWithRecovery(cfg *config.Config, header *vault.Header) (*vault.Vault, error) {
	password, err := crypto.PromptPassphrase("Enter password: ")
	if err != nil {
		return nil, err
	}

	fmt.Fprint(os.Stderr, "Enter recovery code: ")
	var codeStr string
	fmt.Scanln(&codeStr)

	recoveryCode, err := crypto.DecodeRecoveryCode(codeStr)
	if err != nil {
		return nil, fmt.Errorf("invalid recovery code: %w", err)
	}

	_, ageIdentity, err := header.OpenWithRecovery(password, recoveryCode)
	if err != nil {
		return nil, fmt.Errorf("unlocking vault: %w", err)
	}

	v, err := vault.OpenV2(cfg.VaultDir, ageIdentity, header.AgeRecipient)
	if err != nil {
		return nil, fmt.Errorf("opening vault: %w", err)
	}

	return v, nil
}

func openWithMnemonic(cfg *config.Config, header *vault.Header) (*vault.Vault, error) {
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

	v, err := vault.OpenV2(cfg.VaultDir, ageIdentity, header.AgeRecipient)
	if err != nil {
		return nil, fmt.Errorf("opening vault: %w", err)
	}

	return v, nil
}
