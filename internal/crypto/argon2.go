package crypto

import (
	"crypto/sha256"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/hkdf"
)

// Argon2Params holds the parameters for Argon2id key derivation.
type Argon2Params struct {
	Time      uint32 `json:"time"`
	MemoryKiB uint32 `json:"memory_kib"`
	Threads   uint8  `json:"threads"`
}

// MnemonicSlotParams returns aggressive Argon2id parameters for the mnemonic slot.
// Designed to be slow (~5-10s) since it's used rarely (receiver access).
func MnemonicSlotParams() Argon2Params {
	return Argon2Params{
		Time:      4,
		MemoryKiB: 1048576, // 1 GiB
		Threads:   4,
	}
}

// OwnerSlotParams returns Argon2id parameters for the owner/recovery slots.
// Balanced for daily use (~1s).
func OwnerSlotParams() Argon2Params {
	return Argon2Params{
		Time:      2,
		MemoryKiB: 262144, // 256 MiB
		Threads:   4,
	}
}

// DeviceKeyParams returns Argon2id parameters for encrypting the device key file.
// Same as owner slot since it's used on every vault open.
func DeviceKeyParams() Argon2Params {
	return OwnerSlotParams()
}

// MinArgon2Time is the minimum acceptable Argon2id time parameter.
const MinArgon2Time = 1

// MinArgon2MemoryKiB is the minimum acceptable Argon2id memory parameter (64 MiB).
const MinArgon2MemoryKiB = 65536

// ValidateArgon2Params checks that Argon2id parameters meet minimum security thresholds.
// This prevents KDF downgrade attacks where an attacker modifies stored params.
func ValidateArgon2Params(params Argon2Params) error {
	if params.Time < MinArgon2Time {
		return fmt.Errorf("argon2 time parameter too low: %d (minimum %d)", params.Time, MinArgon2Time)
	}
	if params.MemoryKiB < MinArgon2MemoryKiB {
		return fmt.Errorf("argon2 memory parameter too low: %d KiB (minimum %d KiB)", params.MemoryKiB, MinArgon2MemoryKiB)
	}
	if params.Threads < 1 {
		return fmt.Errorf("argon2 threads parameter too low: %d (minimum 1)", params.Threads)
	}
	return nil
}

// TestParams returns minimum-acceptable Argon2id parameters for use in tests.
// Do not use in production code.
func TestParams() Argon2Params {
	return Argon2Params{Time: 1, MemoryKiB: 65536, Threads: 1}
}

// DeriveKey derives a 32-byte key from input material using Argon2id.
func DeriveKey(input, salt []byte, params Argon2Params) ([]byte, error) {
	if len(salt) < 16 {
		return nil, fmt.Errorf("salt must be at least 16 bytes, got %d", len(salt))
	}
	if err := ValidateArgon2Params(params); err != nil {
		return nil, err
	}

	key := argon2.IDKey(input, salt, params.Time, params.MemoryKiB, params.Threads, 32)
	return key, nil
}

// DeriveOwnerSlotKey derives a slot key by combining a password-derived key with a device key via HKDF.
// This ensures both the password AND the device key are required to produce the slot key.
func DeriveOwnerSlotKey(passwordDerivedKey, deviceKey, salt []byte) ([]byte, error) {
	if len(passwordDerivedKey) != 32 {
		return nil, fmt.Errorf("password-derived key must be 32 bytes, got %d", len(passwordDerivedKey))
	}
	if len(deviceKey) != 32 {
		return nil, fmt.Errorf("device key must be 32 bytes, got %d", len(deviceKey))
	}

	// Combine password-derived key and device key as HKDF input key material
	ikm := make([]byte, 64)
	copy(ikm[:32], passwordDerivedKey)
	copy(ikm[32:], deviceKey)
	defer ZeroBytes(ikm)

	hkdfReader := hkdf.New(sha256.New, ikm, salt, []byte("kawarimi-owner-slot"))
	slotKey := make([]byte, 32)
	if _, err := io.ReadFull(hkdfReader, slotKey); err != nil {
		return nil, fmt.Errorf("HKDF expansion: %w", err)
	}

	return slotKey, nil
}
