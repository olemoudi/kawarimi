package deadswitch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"filippo.io/age"
)

func marshalJSON(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

func unmarshalJSON(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

// generateX25519KeyPair generates an age X25519 key pair.
// Returns (publicKey, privateKey/identity).
func generateX25519KeyPair() (string, string, error) {
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		return "", "", fmt.Errorf("generating X25519 identity: %w", err)
	}
	return identity.Recipient().String(), identity.String(), nil
}

// encryptWithX25519 encrypts data with an age X25519 public key.
func encryptWithX25519(plaintext []byte, publicKey string) ([]byte, error) {
	recipient, err := age.ParseX25519Recipient(publicKey)
	if err != nil {
		return nil, fmt.Errorf("parsing recipient: %w", err)
	}

	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, recipient)
	if err != nil {
		return nil, fmt.Errorf("initializing encryption: %w", err)
	}
	if _, err := w.Write(plaintext); err != nil {
		return nil, fmt.Errorf("writing: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("closing: %w", err)
	}

	return buf.Bytes(), nil
}

// decryptWithX25519 decrypts data with an age X25519 identity (private key).
func decryptWithX25519(ciphertext []byte, identityStr string) (string, error) {
	identity, err := age.ParseX25519Identity(identityStr)
	if err != nil {
		return "", fmt.Errorf("parsing identity: %w", err)
	}

	r, err := age.Decrypt(bytes.NewReader(ciphertext), identity)
	if err != nil {
		return "", fmt.Errorf("decrypting: %w", err)
	}

	plaintext, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("reading: %w", err)
	}

	return string(plaintext), nil
}

// pubKeyFromIdentity extracts the public key from an X25519 identity string.
func pubKeyFromIdentity(identityStr string) (string, error) {
	// age identities start with AGE-SECRET-KEY-
	if !strings.HasPrefix(identityStr, "AGE-SECRET-KEY-") {
		return "", fmt.Errorf("invalid identity format")
	}

	identity, err := age.ParseX25519Identity(identityStr)
	if err != nil {
		return "", fmt.Errorf("parsing identity: %w", err)
	}

	return identity.Recipient().String(), nil
}
