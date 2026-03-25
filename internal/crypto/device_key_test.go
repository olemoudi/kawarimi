package crypto_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/olemoudi/kawarimi/internal/crypto"
)

func TestGenerateDeviceKey(t *testing.T) {
	key, err := crypto.GenerateDeviceKey()
	if err != nil {
		t.Fatalf("GenerateDeviceKey failed: %v", err)
	}

	if len(key) != 32 {
		t.Fatalf("expected 32-byte key, got %d", len(key))
	}

	// Should not be all zeros
	allZero := true
	for _, b := range key {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Fatal("generated key should not be all zeros")
	}
}

func TestEncryptDecryptDeviceKey(t *testing.T) {
	deviceKey, err := crypto.GenerateDeviceKey()
	if err != nil {
		t.Fatal(err)
	}

	password := "test-password-123"

	dkf, err := crypto.EncryptDeviceKeyWithParams(deviceKey, password, crypto.TestParams())
	if err != nil {
		t.Fatalf("EncryptDeviceKey failed: %v", err)
	}

	if dkf.Version != 1 {
		t.Fatalf("expected version 1, got %d", dkf.Version)
	}
	if dkf.KDF != "argon2id" {
		t.Fatalf("expected kdf argon2id, got %s", dkf.KDF)
	}

	// Encrypted key should not be the plaintext key
	if bytes.Equal(dkf.EncryptedKey, deviceKey) {
		t.Fatal("encrypted key should differ from plaintext")
	}

	// Decrypt with correct password
	decrypted, err := crypto.DecryptDeviceKey(dkf, password)
	if err != nil {
		t.Fatalf("DecryptDeviceKey failed: %v", err)
	}

	if !bytes.Equal(decrypted, deviceKey) {
		t.Fatal("decrypted key does not match original")
	}
}

func TestDecryptDeviceKeyWrongPassword(t *testing.T) {
	deviceKey, _ := crypto.GenerateDeviceKey()
	dkf, _ := crypto.EncryptDeviceKeyWithParams(deviceKey, "correct-password", crypto.TestParams())

	_, err := crypto.DecryptDeviceKey(dkf, "wrong-password")
	if err == nil {
		t.Fatal("expected error when decrypting with wrong password")
	}
}

func TestSaveLoadDeviceKeyFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "device.key")

	deviceKey, _ := crypto.GenerateDeviceKey()
	dkf, _ := crypto.EncryptDeviceKeyWithParams(deviceKey, "password", crypto.TestParams())

	if err := crypto.SaveDeviceKeyFile(path, dkf); err != nil {
		t.Fatalf("SaveDeviceKeyFile failed: %v", err)
	}

	// Verify file permissions
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("expected 0600 permissions, got %o", perm)
	}

	// Load and decrypt
	loaded, err := crypto.LoadDeviceKeyFile(path)
	if err != nil {
		t.Fatalf("LoadDeviceKeyFile failed: %v", err)
	}

	decrypted, err := crypto.DecryptDeviceKey(loaded, "password")
	if err != nil {
		t.Fatalf("DecryptDeviceKey after load failed: %v", err)
	}

	if !bytes.Equal(decrypted, deviceKey) {
		t.Fatal("loaded and decrypted key does not match original")
	}
}

func TestLoadDeviceKeyFileNotFound(t *testing.T) {
	_, err := crypto.LoadDeviceKeyFile("/nonexistent/device.key")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
