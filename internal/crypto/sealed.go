package crypto

import (
	"encoding/base64"
	"fmt"
)

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
