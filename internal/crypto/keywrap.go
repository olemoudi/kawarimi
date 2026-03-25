package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
)

// WrapKey encrypts plaintext (typically a master key) with a wrapping key using AES-256-GCM.
// Returns the ciphertext and the randomly generated nonce.
func WrapKey(wrappingKey, plaintext []byte) (ciphertext, nonce []byte, err error) {
	if len(wrappingKey) != 32 {
		return nil, nil, fmt.Errorf("wrapping key must be 32 bytes, got %d", len(wrappingKey))
	}

	block, err := aes.NewCipher(wrappingKey)
	if err != nil {
		return nil, nil, fmt.Errorf("creating AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("creating GCM: %w", err)
	}

	nonce = make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, fmt.Errorf("generating nonce: %w", err)
	}

	ciphertext = gcm.Seal(nil, nonce, plaintext, nil)
	return ciphertext, nonce, nil
}

// UnwrapKey decrypts ciphertext with a wrapping key using AES-256-GCM.
// Returns an error if the key is wrong or data is tampered.
func UnwrapKey(wrappingKey, ciphertext, nonce []byte) ([]byte, error) {
	if len(wrappingKey) != 32 {
		return nil, fmt.Errorf("wrapping key must be 32 bytes, got %d", len(wrappingKey))
	}

	block, err := aes.NewCipher(wrappingKey)
	if err != nil {
		return nil, fmt.Errorf("creating AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("creating GCM: %w", err)
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypting: %w", err)
	}

	return plaintext, nil
}

// ZeroBytes overwrites a byte slice with zeros. Best-effort memory zeroing.
func ZeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
