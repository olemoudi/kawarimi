package crypto_test

import (
	"bytes"
	"crypto/rand"
	"testing"

	"github.com/olemoudi/kawarimi/internal/crypto"
)

func TestDeriveKey(t *testing.T) {
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		t.Fatal(err)
	}

	// Use minimal params for test speed
	params := crypto.Argon2Params{Time: 1, MemoryKiB: 64 * 1024, Threads: 1}

	key, err := crypto.DeriveKey([]byte("test-password"), salt, params)
	if err != nil {
		t.Fatalf("DeriveKey failed: %v", err)
	}

	if len(key) != 32 {
		t.Fatalf("expected 32-byte key, got %d", len(key))
	}

	// Same input produces same output
	key2, err := crypto.DeriveKey([]byte("test-password"), salt, params)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(key, key2) {
		t.Fatal("same input should produce same key")
	}

	// Different password produces different key
	key3, err := crypto.DeriveKey([]byte("different-password"), salt, params)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(key, key3) {
		t.Fatal("different password should produce different key")
	}

	// Different salt produces different key
	salt2 := make([]byte, 32)
	if _, err := rand.Read(salt2); err != nil {
		t.Fatal(err)
	}
	key4, err := crypto.DeriveKey([]byte("test-password"), salt2, params)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(key, key4) {
		t.Fatal("different salt should produce different key")
	}
}

func TestDeriveKeyShortSalt(t *testing.T) {
	_, err := crypto.DeriveKey([]byte("pass"), []byte("short"), crypto.Argon2Params{Time: 1, MemoryKiB: 64 * 1024, Threads: 1})
	if err == nil {
		t.Fatal("expected error for short salt")
	}
}

func TestDeriveKeyZeroParams(t *testing.T) {
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		t.Fatal(err)
	}

	_, err := crypto.DeriveKey([]byte("pass"), salt, crypto.Argon2Params{})
	if err == nil {
		t.Fatal("expected error for zero params")
	}
}

func TestDeriveOwnerSlotKey(t *testing.T) {
	pwKey := make([]byte, 32)
	deviceKey := make([]byte, 32)
	salt := make([]byte, 32)
	if _, err := rand.Read(pwKey); err != nil {
		t.Fatal(err)
	}
	if _, err := rand.Read(deviceKey); err != nil {
		t.Fatal(err)
	}
	if _, err := rand.Read(salt); err != nil {
		t.Fatal(err)
	}

	slotKey, err := crypto.DeriveOwnerSlotKey(pwKey, deviceKey, salt)
	if err != nil {
		t.Fatalf("DeriveOwnerSlotKey failed: %v", err)
	}

	if len(slotKey) != 32 {
		t.Fatalf("expected 32-byte slot key, got %d", len(slotKey))
	}

	// Deterministic
	slotKey2, err := crypto.DeriveOwnerSlotKey(pwKey, deviceKey, salt)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(slotKey, slotKey2) {
		t.Fatal("same inputs should produce same slot key")
	}

	// Different device key produces different slot key
	deviceKey2 := make([]byte, 32)
	if _, err := rand.Read(deviceKey2); err != nil {
		t.Fatal(err)
	}
	slotKey3, err := crypto.DeriveOwnerSlotKey(pwKey, deviceKey2, salt)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(slotKey, slotKey3) {
		t.Fatal("different device key should produce different slot key")
	}
}

func TestDeriveOwnerSlotKeyInvalidInputs(t *testing.T) {
	salt := make([]byte, 32)

	_, err := crypto.DeriveOwnerSlotKey([]byte("short"), make([]byte, 32), salt)
	if err == nil {
		t.Fatal("expected error for short password key")
	}

	_, err = crypto.DeriveOwnerSlotKey(make([]byte, 32), []byte("short"), salt)
	if err == nil {
		t.Fatal("expected error for short device key")
	}
}

func TestSlotParamsNonZero(t *testing.T) {
	for name, params := range map[string]crypto.Argon2Params{
		"mnemonic":   crypto.MnemonicSlotParams(),
		"owner":      crypto.OwnerSlotParams(),
		"device_key": crypto.DeviceKeyParams(),
	} {
		if params.Time == 0 || params.MemoryKiB == 0 || params.Threads == 0 {
			t.Errorf("%s params have zero fields: %+v", name, params)
		}
	}
}
