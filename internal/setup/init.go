// Package setup holds the onboarding orchestration shared by the CLI (cmd/) and
// the browser GUI (internal/gui). These functions collect no input and print
// nothing — callers gather input (prompts or HTTP) and render the results — so a
// single code path creates vaults and arms the switch and the two cannot drift.
package setup

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/olemoudi/kawarimi/internal/config"
	"github.com/olemoudi/kawarimi/internal/crypto"
	"github.com/olemoudi/kawarimi/internal/vault"
)

// InitOptions parameterizes InitVault.
type InitOptions struct {
	// VaultDir is where the vault is created. Required.
	VaultDir string
	// Password protects daily (owner-slot) access on this device. Required.
	Password string
	// DeviceID labels this device's owner slot. Defaults to the hostname if empty.
	DeviceID string
	// MnemonicKDFParams / OwnerKDFParams override the Argon2id cost for the mnemonic
	// and owner/recovery slots. Leave nil for production defaults; tests pass
	// crypto.TestParams() for speed.
	MnemonicKDFParams *crypto.Argon2Params
	OwnerKDFParams    *crypto.Argon2Params
}

// InitSecrets are the write-these-down-once secrets produced by InitVault. The
// caller is responsible for displaying them exactly once and never persisting
// them.
type InitSecrets struct {
	VaultDir            string
	MnemonicWords       []string
	RecoveryCode        string // formatted for display
	RecipientPassphrase string
	DMSKeyB64           string
	DeviceKeyPath       string
}

// InitVault creates a brand-new V4 vault: header with all slots, the encrypted
// vault, this device's key file, the config, the recipient passphrase, and the
// sealed V4 payload. It returns the one-time secrets to display. It does not
// prompt or print.
func InitVault(opts InitOptions) (*InitSecrets, error) {
	if opts.VaultDir == "" {
		return nil, fmt.Errorf("vault directory is required")
	}
	if opts.Password == "" {
		return nil, fmt.Errorf("password is required")
	}
	// Guard against clobbering an existing installation (the CLI performs its own
	// friendlier check first; this protects non-CLI callers).
	if cfg, err := config.Load(); err == nil {
		return nil, fmt.Errorf("vault already configured at %s", cfg.VaultDir)
	}

	deviceID := opts.DeviceID
	if deviceID == "" {
		if hostname, err := os.Hostname(); err == nil {
			deviceID = hostname
		} else {
			deviceID = "default"
		}
	}

	// Create header with all key material (pure — no I/O).
	result, err := vault.NewHeader(vault.InitParams{
		Password:          opts.Password,
		DeviceID:          deviceID,
		MnemonicKDFParams: opts.MnemonicKDFParams,
		OwnerKDFParams:    opts.OwnerKDFParams,
	})
	if err != nil {
		return nil, fmt.Errorf("creating vault header: %w", err)
	}
	defer crypto.ZeroBytes(result.MasterKey)
	defer crypto.ZeroBytes(result.DeviceKey)
	defer crypto.ZeroBytes(result.RecoveryCode)

	// Persist header + vault.
	if err := os.MkdirAll(opts.VaultDir, 0700); err != nil {
		return nil, fmt.Errorf("creating vault directory: %w", err)
	}
	if err := vault.SaveHeader(opts.VaultDir, result.Header); err != nil {
		return nil, fmt.Errorf("saving vault header: %w", err)
	}
	v, err := vault.CreateV2(opts.VaultDir, result.AgeIdentity, result.Header.AgeRecipient)
	if err != nil {
		return nil, fmt.Errorf("creating vault: %w", err)
	}

	// Save the encrypted device key for this machine.
	appDir, err := config.AppDirPath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(appDir, 0700); err != nil {
		return nil, fmt.Errorf("creating app directory: %w", err)
	}
	dkf, err := crypto.EncryptDeviceKey(result.DeviceKey, opts.Password)
	if err != nil {
		return nil, fmt.Errorf("encrypting device key: %w", err)
	}
	deviceKeyPath := filepath.Join(appDir, "device.key")
	if err := crypto.SaveDeviceKeyFile(deviceKeyPath, dkf); err != nil {
		return nil, fmt.Errorf("saving device key: %w", err)
	}

	// Save config.
	cfg := config.DefaultConfig(v.Dir)
	if err := config.Save(cfg); err != nil {
		return nil, fmt.Errorf("saving config: %w", err)
	}

	// Recipient passphrase + V4 seal.
	recipientPassphrase, err := crypto.GenerateRecipientPassphrase()
	if err != nil {
		return nil, fmt.Errorf("generating recipient passphrase: %w", err)
	}
	mnemonicEntropy, err := crypto.DecodeMnemonic(result.MnemonicWords)
	if err != nil {
		return nil, fmt.Errorf("encoding mnemonic entropy: %w", err)
	}
	dmsKeyB64, err := SealAndInstallV4(opts.VaultDir, appDir, mnemonicEntropy, recipientPassphrase)
	crypto.ZeroBytes(mnemonicEntropy)
	if err != nil {
		return nil, err
	}

	return &InitSecrets{
		VaultDir:            v.Dir,
		MnemonicWords:       result.MnemonicWords,
		RecoveryCode:        crypto.FormatRecoveryCode(result.RecoveryCode),
		RecipientPassphrase: recipientPassphrase,
		DMSKeyB64:           dmsKeyB64,
		DeviceKeyPath:       deviceKeyPath,
	}, nil
}

// SealAndInstallV4 seals the mnemonic entropy under a fresh DMS key plus the given
// recipient passphrase (V4 key-split), writes the sealed payload into the vault, and
// stores the DMS key locally. Returns the DMS key (base64) for display. Shared by
// init and switch rekey so the two cannot drift.
func SealAndInstallV4(vaultDir, appDir string, entropy []byte, recipientPassphrase string) (dmsKeyB64 string, err error) {
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
