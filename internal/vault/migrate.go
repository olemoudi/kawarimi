package vault

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/olemoudi/kawarimi/internal/crypto"
)

// MigrateV1ToV2 migrates a v1 vault (passphrase-based) to v2 (identity-based with slot architecture).
// This is an O(n) operation: every file must be re-encrypted from scrypt to X25519.
// Returns the InitResult containing mnemonic words, recovery code, device key, etc.
// kdfParams is optional — pass nil to use production defaults.
func MigrateV1ToV2(vaultDir, passphrase, password, deviceID string, kdfParams *crypto.Argon2Params) (*InitResult, error) {
	// 1. Open v1 vault
	v1, err := Open(vaultDir, passphrase)
	if err != nil {
		return nil, fmt.Errorf("opening v1 vault: %w", err)
	}

	// 2. Create header (generates MK, age identity, slots, etc.)
	initParams := InitParams{
		Password: password,
		DeviceID: deviceID,
	}
	if kdfParams != nil {
		initParams.MnemonicKDFParams = kdfParams
		initParams.OwnerKDFParams = kdfParams
	}
	result, err := NewHeader(initParams)
	if err != nil {
		return nil, fmt.Errorf("creating vault header: %w", err)
	}

	// 3. Re-encrypt all data files from scrypt to X25519
	var rekeyPaths []string
	for i, entry := range v1.Manifest.Entries {
		data, err := v1.ShowEntry(entry)
		if err != nil {
			cleanupRekey(rekeyPaths)
			return nil, fmt.Errorf("decrypting %s: %w", entry.Filename, err)
		}

		rekeyPath := filepath.Join(vaultDir, entry.Filename+".v2rekey")
		ciphertext, err := EncryptWithIdentity(data, result.Header.AgeRecipient)
		if err != nil {
			cleanupRekey(rekeyPaths)
			return nil, fmt.Errorf("re-encrypting %s: %w", entry.Filename, err)
		}

		if err := os.WriteFile(rekeyPath, ciphertext, 0600); err != nil {
			cleanupRekey(rekeyPaths)
			return nil, fmt.Errorf("writing %s: %w", rekeyPath, err)
		}
		rekeyPaths = append(rekeyPaths, rekeyPath)
		fmt.Printf("  Migrated %d/%d: %s\n", i+1, len(v1.Manifest.Entries), entry.Title)
	}

	// 4. Re-encrypt manifest
	manifestRekeyPath := filepath.Join(vaultDir, ManifestFile+".v2rekey")
	if err := SaveManifestV2(manifestRekeyPath, v1.Manifest, result.Header.AgeRecipient); err != nil {
		cleanupRekey(rekeyPaths)
		os.Remove(manifestRekeyPath)
		return nil, fmt.Errorf("re-encrypting manifest: %w", err)
	}

	// 5. All re-encryption succeeded — atomic rename
	for i, entry := range v1.Manifest.Entries {
		finalPath := filepath.Join(vaultDir, entry.Filename)
		if err := os.Rename(rekeyPaths[i], finalPath); err != nil {
			return nil, fmt.Errorf("renaming %s: %w (vault may be in inconsistent state)", entry.Filename, err)
		}
	}
	if err := os.Rename(manifestRekeyPath, filepath.Join(vaultDir, ManifestFile)); err != nil {
		return nil, fmt.Errorf("renaming manifest: %w", err)
	}

	// 6. Write vault header
	if err := SaveHeader(vaultDir, result.Header); err != nil {
		return nil, fmt.Errorf("saving vault header: %w", err)
	}

	return result, nil
}

// IsV2Vault checks if a vault directory contains a v2 header.
func IsV2Vault(vaultDir string) bool {
	_, err := os.Stat(filepath.Join(vaultDir, HeaderFile))
	return err == nil
}

func cleanupRekey(paths []string) {
	for _, p := range paths {
		os.Remove(p)
	}
}

// RecoveryCodeFromMasterKey decrypts the stored recovery code using the master key.
func RecoveryCodeFromMasterKey(header *Header, masterKey []byte) ([]byte, error) {
	ct, nonce, err := header.GetEncryptedRecoveryCode()
	if err != nil {
		return nil, err
	}
	return crypto.UnwrapKey(masterKey, ct, nonce)
}
