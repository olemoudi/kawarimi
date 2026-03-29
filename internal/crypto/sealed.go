package crypto

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

const DMSKeyBytes = 32

// SealMnemonic encrypts mnemonic entropy with a recipient passphrase using age scrypt.
// The resulting ciphertext can only be decrypted with the same passphrase.
// This is used for the sealed DMS payload: the DMS stores the ciphertext but
// cannot decrypt it (it doesn't have the passphrase).
func SealMnemonic(entropy []byte, recipientPassphrase string) ([]byte, error) {
	if len(entropy) != MnemonicEntropyBytes {
		return nil, fmt.Errorf("entropy must be %d bytes, got %d", MnemonicEntropyBytes, len(entropy))
	}
	if recipientPassphrase == "" {
		return nil, fmt.Errorf("recipient passphrase must not be empty")
	}

	ciphertext, err := Encrypt(entropy, recipientPassphrase)
	if err != nil {
		return nil, fmt.Errorf("sealing mnemonic: %w", err)
	}
	return ciphertext, nil
}

// UnsealMnemonic decrypts a sealed mnemonic payload using the recipient passphrase.
// Returns the raw mnemonic entropy (11 bytes).
func UnsealMnemonic(ciphertext []byte, recipientPassphrase string) ([]byte, error) {
	if len(ciphertext) == 0 {
		return nil, fmt.Errorf("ciphertext is empty")
	}
	if recipientPassphrase == "" {
		return nil, fmt.Errorf("recipient passphrase must not be empty")
	}

	entropy, err := Decrypt(ciphertext, recipientPassphrase)
	if err != nil {
		return nil, fmt.Errorf("unsealing mnemonic: %w", err)
	}

	if len(entropy) != MnemonicEntropyBytes {
		return nil, fmt.Errorf("unsealed data has wrong length: expected %d bytes, got %d", MnemonicEntropyBytes, len(entropy))
	}

	return entropy, nil
}

// EncodeSealedPayload encodes sealed ciphertext as a base64 string for transport (email, etc.).
func EncodeSealedPayload(ciphertext []byte) string {
	return base64.StdEncoding.EncodeToString(ciphertext)
}

// DecodeSealedPayload decodes a base64-encoded sealed payload back to ciphertext bytes.
func DecodeSealedPayload(encoded string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decoding sealed payload: %w", err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("decoded sealed payload is empty")
	}
	return data, nil
}

// GenerateDMSKey generates a random 32-byte DMS key for the V4 key-split architecture.
func GenerateDMSKey() ([]byte, error) {
	key := make([]byte, DMSKeyBytes)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generating DMS key: %w", err)
	}
	return key, nil
}

// EncodeDMSKey encodes a DMS key as base64 for email transport.
func EncodeDMSKey(key []byte) string {
	return base64.StdEncoding.EncodeToString(key)
}

// DecodeDMSKey decodes a base64-encoded DMS key back to raw bytes.
func DecodeDMSKey(encoded string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decoding DMS key: %w", err)
	}
	if len(data) != DMSKeyBytes {
		return nil, fmt.Errorf("DMS key must be %d bytes, got %d", DMSKeyBytes, len(data))
	}
	return data, nil
}

// CombinePassphrase constructs the combined passphrase from a DMS key and recipient passphrase.
// Format: base64(dmsKey) + ":" + recipientPassphrase
// The combined string is used as the age scrypt passphrase for V4 sealing.
func CombinePassphrase(dmsKey []byte, recipientPassphrase string) (string, error) {
	if len(dmsKey) != DMSKeyBytes {
		return "", fmt.Errorf("DMS key must be %d bytes, got %d", DMSKeyBytes, len(dmsKey))
	}
	if recipientPassphrase == "" {
		return "", fmt.Errorf("recipient passphrase must not be empty")
	}
	return base64.StdEncoding.EncodeToString(dmsKey) + ":" + recipientPassphrase, nil
}

// SealMnemonicV4 encrypts mnemonic entropy with a combined DMS key + recipient passphrase.
// This is the V4 key-split architecture: both the DMS key (delivered via email on trigger)
// and the recipient passphrase (on physical card) are required to unseal.
func SealMnemonicV4(entropy []byte, dmsKey []byte, recipientPassphrase string) ([]byte, error) {
	combined, err := CombinePassphrase(dmsKey, recipientPassphrase)
	if err != nil {
		return nil, fmt.Errorf("combining passphrase: %w", err)
	}
	return SealMnemonic(entropy, combined)
}

// UnsealMnemonicV4 decrypts a V4-sealed mnemonic payload using DMS key + recipient passphrase.
func UnsealMnemonicV4(ciphertext []byte, dmsKey []byte, recipientPassphrase string) ([]byte, error) {
	combined, err := CombinePassphrase(dmsKey, recipientPassphrase)
	if err != nil {
		return nil, fmt.Errorf("combining passphrase: %w", err)
	}
	return UnsealMnemonic(ciphertext, combined)
}
