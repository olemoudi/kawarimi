package crypto

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"filippo.io/age"
	"github.com/olemoudi/kawarimi/internal/atomicfile"
)

// ScryptWorkFactor overrides the age scrypt work factor for v1 encryption.
// 0 means use the library default (production). Set to a lower value (e.g., 10) in tests.
// This only affects the deprecated v1 passphrase-based encryption.
var ScryptWorkFactor int

// Encrypt encrypts plaintext bytes with the given passphrase using age scrypt.
func Encrypt(plaintext []byte, passphrase string) ([]byte, error) {
	recipient, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return nil, fmt.Errorf("creating scrypt recipient: %w", err)
	}
	if ScryptWorkFactor > 0 {
		recipient.SetWorkFactor(ScryptWorkFactor)
	}

	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, recipient)
	if err != nil {
		return nil, fmt.Errorf("initializing encryption: %w", err)
	}
	if _, err := w.Write(plaintext); err != nil {
		return nil, fmt.Errorf("writing encrypted data: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("finalizing encryption: %w", err)
	}

	return buf.Bytes(), nil
}

// Decrypt decrypts age-encrypted ciphertext with the given passphrase.
func Decrypt(ciphertext []byte, passphrase string) ([]byte, error) {
	identity, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return nil, fmt.Errorf("creating scrypt identity: %w", err)
	}

	r, err := age.Decrypt(bytes.NewReader(ciphertext), identity)
	if err != nil {
		return nil, fmt.Errorf("decrypting: %w", err)
	}

	plaintext, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading decrypted data: %w", err)
	}

	return plaintext, nil
}

// EncryptFile encrypts plaintext bytes and writes the result to the given path.
func EncryptFile(path string, plaintext []byte, passphrase string) error {
	ciphertext, err := Encrypt(plaintext, passphrase)
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(path, ciphertext, 0600)
}

// DecryptFile reads an encrypted file and returns the decrypted plaintext.
func DecryptFile(path string, passphrase string) ([]byte, error) {
	ciphertext, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading file %s: %w", path, err)
	}
	return Decrypt(ciphertext, passphrase)
}
