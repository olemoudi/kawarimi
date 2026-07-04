package vault

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/olemoudi/kawarimi/internal/atomicfile"
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

		if err := atomicfile.WriteFile(rekeyPath, ciphertext, 0600); err != nil {
			cleanupRekey(rekeyPaths)
			return nil, fmt.Errorf("writing %s: %w", rekeyPath, err)
		}
		rekeyPaths = append(rekeyPaths, rekeyPath)
		fmt.Printf("  Migrated %d/%d: %s\n", i+1, len(v1.Manifest.Entries), entry.Title)
	}

	// 4. Re-encrypt manifest to a side file (v1 originals still untouched).
	manifestRekeyPath := filepath.Join(vaultDir, ManifestFile+".v2rekey")
	if err := SaveManifestV2(manifestRekeyPath, v1.Manifest, result.Header.AgeRecipient); err != nil {
		cleanupRekey(rekeyPaths)
		os.Remove(manifestRekeyPath)
		return nil, fmt.Errorf("re-encrypting manifest: %w", err)
	}

	// 5. Persist the header FIRST. It holds the ONLY copy of the new age identity;
	//    writing it before we disturb any original guarantees that if we crash
	//    during the swap below, the identity that decrypts the .v2rekey files is
	//    already durable and the v1 originals (or their .v1bak copies) still exist.
	if err := SaveHeader(vaultDir, result.Header); err != nil {
		cleanupRekey(rekeyPaths)
		os.Remove(manifestRekeyPath)
		return nil, fmt.Errorf("saving vault header: %w", err)
	}

	// 6. Swap each migrated file into place, preserving the v1 original as .v1bak
	//    until the migrated vault has been verified. Nothing v1 is deleted yet.
	var backups []string
	swap := func(finalName, rekeyPath string) error {
		finalPath := filepath.Join(vaultDir, finalName)
		bakPath := finalPath + ".v1bak"
		if err := os.Rename(finalPath, bakPath); err != nil {
			return err
		}
		backups = append(backups, bakPath)
		return os.Rename(rekeyPath, finalPath)
	}
	rollback := func() {
		for _, bak := range backups {
			_ = os.Rename(bak, bak[:len(bak)-len(".v1bak")])
		}
		// A v1 vault has no header — remove the one we wrote so the vault is a clean
		// v1 again. Also drop any leftover .v2rekey side files.
		os.Remove(filepath.Join(vaultDir, HeaderFile))
		os.Remove(filepath.Join(vaultDir, HeaderFile+".bak"))
		cleanupRekey(rekeyPaths)
		os.Remove(manifestRekeyPath)
	}
	for i, entry := range v1.Manifest.Entries {
		if err := swap(entry.Filename, rekeyPaths[i]); err != nil {
			rollback()
			return nil, fmt.Errorf("swapping %s: %w", entry.Filename, err)
		}
	}
	if err := swap(ManifestFile, manifestRekeyPath); err != nil {
		rollback()
		return nil, fmt.Errorf("swapping manifest: %w", err)
	}

	// 7. Verify the migrated vault opens and every entry decrypts BEFORE discarding
	//    the v1 originals. If anything is wrong, roll back to the intact v1 vault.
	v2, err := OpenV2(vaultDir, result.AgeIdentity, result.Header.AgeRecipient)
	if err != nil {
		rollback()
		return nil, fmt.Errorf("migrated vault failed to open (rolled back): %w", err)
	}
	if errs := v2.Verify(); len(errs) > 0 {
		rollback()
		return nil, fmt.Errorf("migrated vault failed verification (rolled back): %v", errs[0])
	}

	// 8. Success — remove the v1 backups.
	for _, bak := range backups {
		os.Remove(bak)
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
