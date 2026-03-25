package vault_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/olemoudi/kawarimi/internal/crypto"
	"github.com/olemoudi/kawarimi/internal/vault"
)

// testInitParams returns params with fast KDF for testing.
func testInitParams() vault.InitParams {
	tp := crypto.TestParams()
	return vault.InitParams{
		Password:          "test-password-123",
		DeviceID:          "test-device",
		MnemonicKDFParams: &tp,
		OwnerKDFParams:    &tp,
	}
}

func TestNewHeader(t *testing.T) {
	result, err := vault.NewHeader(testInitParams())
	if err != nil {
		t.Fatalf("NewHeader failed: %v", err)
	}
	defer crypto.ZeroBytes(result.MasterKey)

	if result.Header.Version != 2 {
		t.Fatalf("expected version 2, got %d", result.Header.Version)
	}
	if len(result.Header.Slots) != 3 {
		t.Fatalf("expected 3 slots, got %d", len(result.Header.Slots))
	}
	if len(result.MnemonicWords) != 8 {
		t.Fatalf("expected 8 mnemonic words, got %d", len(result.MnemonicWords))
	}
	if len(result.RecoveryCode) != 16 {
		t.Fatalf("expected 16-byte recovery code, got %d", len(result.RecoveryCode))
	}
	if len(result.DeviceKey) != 32 {
		t.Fatalf("expected 32-byte device key, got %d", len(result.DeviceKey))
	}
	if len(result.MasterKey) != 32 {
		t.Fatalf("expected 32-byte master key, got %d", len(result.MasterKey))
	}
	if result.AgeIdentity == "" {
		t.Fatal("expected non-empty age identity")
	}
	if result.Header.AgeRecipient == "" {
		t.Fatal("expected non-empty age recipient")
	}
	if len(result.Header.HeaderHMAC) == 0 {
		t.Fatal("expected non-empty header HMAC")
	}

	// Verify slot types
	types := map[vault.SlotType]bool{}
	for _, s := range result.Header.Slots {
		types[s.Type] = true
	}
	if !types[vault.SlotTypeMnemonic] || !types[vault.SlotTypeOwner] || !types[vault.SlotTypeRecovery] {
		t.Fatal("expected one slot of each type")
	}
}

func TestOpenWithMnemonic(t *testing.T) {
	result, err := vault.NewHeader(testInitParams())
	if err != nil {
		t.Fatal(err)
	}
	defer crypto.ZeroBytes(result.MasterKey)

	mk, identity, err := result.Header.OpenWithMnemonic(result.MnemonicWords)
	if err != nil {
		t.Fatalf("OpenWithMnemonic failed: %v", err)
	}
	defer crypto.ZeroBytes(mk)

	if !bytes.Equal(mk, result.MasterKey) {
		t.Fatal("master key mismatch")
	}
	if identity != result.AgeIdentity {
		t.Fatal("age identity mismatch")
	}
}

func TestOpenWithMnemonicWrongWords(t *testing.T) {
	result, _ := vault.NewHeader(testInitParams())
	defer crypto.ZeroBytes(result.MasterKey)

	wrongWords := []string{"abandon", "abandon", "abandon", "abandon", "abandon", "abandon", "abandon", "abandon"}
	_, _, err := result.Header.OpenWithMnemonic(wrongWords)
	if err == nil {
		t.Fatal("expected error with wrong mnemonic")
	}
}

func TestOpenWithOwner(t *testing.T) {
	params := testInitParams()
	result, err := vault.NewHeader(params)
	if err != nil {
		t.Fatal(err)
	}
	defer crypto.ZeroBytes(result.MasterKey)

	mk, identity, err := result.Header.OpenWithOwner(params.Password, result.DeviceKey)
	if err != nil {
		t.Fatalf("OpenWithOwner failed: %v", err)
	}
	defer crypto.ZeroBytes(mk)

	if !bytes.Equal(mk, result.MasterKey) {
		t.Fatal("master key mismatch")
	}
	if identity != result.AgeIdentity {
		t.Fatal("age identity mismatch")
	}
}

func TestOpenWithOwnerWrongPassword(t *testing.T) {
	result, _ := vault.NewHeader(testInitParams())
	defer crypto.ZeroBytes(result.MasterKey)

	_, _, err := result.Header.OpenWithOwner("wrong-password", result.DeviceKey)
	if err == nil {
		t.Fatal("expected error with wrong password")
	}
}

func TestOpenWithOwnerWrongDeviceKey(t *testing.T) {
	params := testInitParams()
	result, _ := vault.NewHeader(params)
	defer crypto.ZeroBytes(result.MasterKey)

	wrongDeviceKey := make([]byte, 32)
	_, _, err := result.Header.OpenWithOwner(params.Password, wrongDeviceKey)
	if err == nil {
		t.Fatal("expected error with wrong device key")
	}
}

func TestOpenWithRecovery(t *testing.T) {
	params := testInitParams()
	result, err := vault.NewHeader(params)
	if err != nil {
		t.Fatal(err)
	}
	defer crypto.ZeroBytes(result.MasterKey)

	mk, identity, err := result.Header.OpenWithRecovery(params.Password, result.RecoveryCode)
	if err != nil {
		t.Fatalf("OpenWithRecovery failed: %v", err)
	}
	defer crypto.ZeroBytes(mk)

	if !bytes.Equal(mk, result.MasterKey) {
		t.Fatal("master key mismatch")
	}
	if identity != result.AgeIdentity {
		t.Fatal("age identity mismatch")
	}
}

func TestOpenWithRecoveryWrongCode(t *testing.T) {
	params := testInitParams()
	result, _ := vault.NewHeader(params)
	defer crypto.ZeroBytes(result.MasterKey)

	wrongCode := make([]byte, 16)
	_, _, err := result.Header.OpenWithRecovery(params.Password, wrongCode)
	if err == nil {
		t.Fatal("expected error with wrong recovery code")
	}
}

func TestCrossSlotConsistency(t *testing.T) {
	params := testInitParams()
	result, _ := vault.NewHeader(params)
	defer crypto.ZeroBytes(result.MasterKey)

	// All three slots should produce the same master key and identity
	mk1, id1, _ := result.Header.OpenWithMnemonic(result.MnemonicWords)
	defer crypto.ZeroBytes(mk1)
	mk2, id2, _ := result.Header.OpenWithOwner(params.Password, result.DeviceKey)
	defer crypto.ZeroBytes(mk2)
	mk3, id3, _ := result.Header.OpenWithRecovery(params.Password, result.RecoveryCode)
	defer crypto.ZeroBytes(mk3)

	if !bytes.Equal(mk1, mk2) || !bytes.Equal(mk2, mk3) {
		t.Fatal("master keys from different slots don't match")
	}
	if id1 != id2 || id2 != id3 {
		t.Fatal("age identities from different slots don't match")
	}
}

func TestUpdateOwnerSlot(t *testing.T) {
	params := testInitParams()
	result, _ := vault.NewHeader(params)
	defer crypto.ZeroBytes(result.MasterKey)

	newPassword := "new-password-456"
	err := result.Header.UpdateOwnerSlot(params.DeviceID, newPassword, result.DeviceKey, result.MasterKey, params.OwnerKDFParams)
	if err != nil {
		t.Fatalf("UpdateOwnerSlot failed: %v", err)
	}

	// Old password should fail
	_, _, err = result.Header.OpenWithOwner(params.Password, result.DeviceKey)
	if err == nil {
		t.Fatal("old password should not work after update")
	}

	// New password should work
	mk, _, err := result.Header.OpenWithOwner(newPassword, result.DeviceKey)
	if err != nil {
		t.Fatalf("new password should work: %v", err)
	}
	defer crypto.ZeroBytes(mk)

	if !bytes.Equal(mk, result.MasterKey) {
		t.Fatal("master key mismatch after owner slot update")
	}
}

func TestUpdateRecoverySlot(t *testing.T) {
	params := testInitParams()
	result, _ := vault.NewHeader(params)
	defer crypto.ZeroBytes(result.MasterKey)

	newPassword := "new-password-789"
	err := result.Header.UpdateRecoverySlot(newPassword, result.RecoveryCode, result.MasterKey, params.OwnerKDFParams)
	if err != nil {
		t.Fatalf("UpdateRecoverySlot failed: %v", err)
	}

	// New password + same recovery code should work
	mk, _, err := result.Header.OpenWithRecovery(newPassword, result.RecoveryCode)
	if err != nil {
		t.Fatalf("new password with recovery should work: %v", err)
	}
	defer crypto.ZeroBytes(mk)

	if !bytes.Equal(mk, result.MasterKey) {
		t.Fatal("master key mismatch after recovery slot update")
	}
}

func TestAddOwnerSlot(t *testing.T) {
	params := testInitParams()
	result, _ := vault.NewHeader(params)
	defer crypto.ZeroBytes(result.MasterKey)

	// Add a second device
	newDeviceKey, _ := crypto.GenerateDeviceKey()
	err := result.Header.AddOwnerSlot(params.Password, newDeviceKey, "second-device", result.MasterKey, params.OwnerKDFParams)
	if err != nil {
		t.Fatalf("AddOwnerSlot failed: %v", err)
	}

	if len(result.Header.Slots) != 4 {
		t.Fatalf("expected 4 slots, got %d", len(result.Header.Slots))
	}

	// Both devices should work
	mk1, _, err := result.Header.OpenWithOwner(params.Password, result.DeviceKey)
	if err != nil {
		t.Fatalf("original device should still work: %v", err)
	}
	defer crypto.ZeroBytes(mk1)

	mk2, _, err := result.Header.OpenWithOwner(params.Password, newDeviceKey)
	if err != nil {
		t.Fatalf("new device should work: %v", err)
	}
	defer crypto.ZeroBytes(mk2)

	if !bytes.Equal(mk1, mk2) {
		t.Fatal("both devices should produce same master key")
	}
}

func TestGetEncryptedRecoveryCode(t *testing.T) {
	result, _ := vault.NewHeader(testInitParams())
	defer crypto.ZeroBytes(result.MasterKey)

	ct, nonce, err := result.Header.GetEncryptedRecoveryCode()
	if err != nil {
		t.Fatalf("GetEncryptedRecoveryCode failed: %v", err)
	}

	// Decrypt with MK
	decrypted, err := crypto.UnwrapKey(result.MasterKey, ct, nonce)
	if err != nil {
		t.Fatalf("decrypting recovery code failed: %v", err)
	}

	if !bytes.Equal(decrypted, result.RecoveryCode) {
		t.Fatal("decrypted recovery code doesn't match original")
	}
}

func TestSaveLoadHeader(t *testing.T) {
	tmpDir := t.TempDir()
	result, _ := vault.NewHeader(testInitParams())
	defer crypto.ZeroBytes(result.MasterKey)

	if err := vault.SaveHeader(tmpDir, result.Header); err != nil {
		t.Fatalf("SaveHeader failed: %v", err)
	}

	loaded, err := vault.LoadHeader(tmpDir)
	if err != nil {
		t.Fatalf("LoadHeader failed: %v", err)
	}

	// Verify it can still be opened
	mk, _, err := loaded.OpenWithMnemonic(result.MnemonicWords)
	if err != nil {
		t.Fatalf("loaded header OpenWithMnemonic failed: %v", err)
	}
	defer crypto.ZeroBytes(mk)

	if !bytes.Equal(mk, result.MasterKey) {
		t.Fatal("master key mismatch after save/load")
	}
}

func TestLoadHeaderNotFound(t *testing.T) {
	_, err := vault.LoadHeader("/nonexistent")
	if err == nil {
		t.Fatal("expected error for missing header")
	}
}

func TestHeaderHMACTamperDetection(t *testing.T) {
	result, _ := vault.NewHeader(testInitParams())
	defer crypto.ZeroBytes(result.MasterKey)

	// Tamper with a slot's encrypted master key
	result.Header.Slots[0].EncryptedMasterKey[0] ^= 0xFF

	_, _, err := result.Header.OpenWithMnemonic(result.MnemonicWords)
	if err == nil {
		t.Fatal("expected error after tampering with header")
	}
}

func TestEncryptDecryptWithIdentity(t *testing.T) {
	result, _ := vault.NewHeader(testInitParams())
	defer crypto.ZeroBytes(result.MasterKey)

	plaintext := []byte("secret vault data")

	ciphertext, err := vault.EncryptWithIdentity(plaintext, result.Header.AgeRecipient)
	if err != nil {
		t.Fatalf("EncryptWithIdentity failed: %v", err)
	}

	if bytes.Equal(ciphertext, plaintext) {
		t.Fatal("ciphertext should differ from plaintext")
	}

	decrypted, err := vault.DecryptWithIdentity(ciphertext, result.AgeIdentity)
	if err != nil {
		t.Fatalf("DecryptWithIdentity failed: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Fatal("decrypted data doesn't match original")
	}
}

func TestEncryptDecryptWithIdentityWrongKey(t *testing.T) {
	result, _ := vault.NewHeader(testInitParams())
	defer crypto.ZeroBytes(result.MasterKey)

	// Create a different identity
	result2, _ := vault.NewHeader(testInitParams())
	defer crypto.ZeroBytes(result2.MasterKey)

	ciphertext, _ := vault.EncryptWithIdentity([]byte("secret"), result.Header.AgeRecipient)

	_, err := vault.DecryptWithIdentity(ciphertext, result2.AgeIdentity)
	if err == nil {
		t.Fatal("expected error decrypting with wrong identity")
	}
}

func TestSaveHeaderFilePermissions(t *testing.T) {
	tmpDir := t.TempDir()
	result, _ := vault.NewHeader(testInitParams())
	defer crypto.ZeroBytes(result.MasterKey)

	if err := vault.SaveHeader(tmpDir, result.Header); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(filepath.Join(tmpDir, vault.HeaderFile))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("expected 0600 permissions, got %o", perm)
	}
}
