package vault_test

import (
	"testing"

	"github.com/olemoudi/kawarimi/internal/crypto"
	"github.com/olemoudi/kawarimi/internal/vault"
)

func TestMigrateV1ToV2(t *testing.T) {
	dir := t.TempDir()
	oldPassphrase := "v1-passphrase"

	// Create v1 vault with some entries
	v1, err := vault.Create(dir, oldPassphrase)
	if err != nil {
		t.Fatalf("Create v1: %v", err)
	}

	v1.AddNote("Test Note", []byte("secret note content"), []string{"tag1"})
	v1.AddCredential(&vault.Credential{
		Service:  "TestService",
		Username: "user",
		Password: "pass123",
	}, nil)

	// Verify v1 vault is NOT v2
	if vault.IsV2Vault(dir) {
		t.Fatal("should not be v2 before migration")
	}

	// Migrate
	newPassword := "v2-password"
	result, err := vault.MigrateV1ToV2(dir, oldPassphrase, newPassword, "test-device")
	if err != nil {
		t.Fatalf("MigrateV1ToV2: %v", err)
	}
	defer crypto.ZeroBytes(result.MasterKey)

	// Verify v2 vault exists
	if !vault.IsV2Vault(dir) {
		t.Fatal("should be v2 after migration")
	}

	// Verify we got mnemonic words, recovery code, device key
	if len(result.MnemonicWords) != 8 {
		t.Fatalf("expected 8 mnemonic words, got %d", len(result.MnemonicWords))
	}
	if len(result.RecoveryCode) != 16 {
		t.Fatalf("expected 16-byte recovery code, got %d", len(result.RecoveryCode))
	}
	if len(result.DeviceKey) != 32 {
		t.Fatalf("expected 32-byte device key, got %d", len(result.DeviceKey))
	}

	// Open with owner slot
	header, err := vault.LoadHeader(dir)
	if err != nil {
		t.Fatalf("LoadHeader: %v", err)
	}

	mk, identity, err := header.OpenWithOwner(newPassword, result.DeviceKey)
	if err != nil {
		t.Fatalf("OpenWithOwner: %v", err)
	}
	defer crypto.ZeroBytes(mk)

	v2, err := vault.OpenV2(dir, identity, header.AgeRecipient)
	if err != nil {
		t.Fatalf("OpenV2: %v", err)
	}

	// Verify all entries migrated and decrypt correctly
	if len(v2.Manifest.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(v2.Manifest.Entries))
	}

	for _, e := range v2.Manifest.Entries {
		data, err := v2.ShowEntry(e)
		if err != nil {
			t.Fatalf("ShowEntry %s: %v", e.Title, err)
		}
		if len(data) == 0 {
			t.Fatalf("empty content for %s", e.Title)
		}
	}

	// Open with mnemonic
	mk2, identity2, err := header.OpenWithMnemonic(result.MnemonicWords)
	if err != nil {
		t.Fatalf("OpenWithMnemonic: %v", err)
	}
	defer crypto.ZeroBytes(mk2)

	v3, err := vault.OpenV2(dir, identity2, header.AgeRecipient)
	if err != nil {
		t.Fatalf("OpenV2 mnemonic: %v", err)
	}

	for _, e := range v3.Manifest.Entries {
		if _, err := v3.ShowEntry(e); err != nil {
			t.Fatalf("ShowEntry via mnemonic %s: %v", e.Title, err)
		}
	}

	// Old passphrase should no longer work (files are now X25519-encrypted)
	_, err = vault.Open(dir, oldPassphrase)
	if err == nil {
		t.Fatal("old passphrase should not work after migration")
	}
}

func TestIsV2Vault(t *testing.T) {
	dir := t.TempDir()

	if vault.IsV2Vault(dir) {
		t.Fatal("empty dir should not be v2")
	}

	// Create v1 vault
	vault.Create(dir, "pass")
	if vault.IsV2Vault(dir) {
		t.Fatal("v1 vault should not be v2")
	}
}
