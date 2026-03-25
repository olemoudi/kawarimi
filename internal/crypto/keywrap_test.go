package crypto_test

import (
	"bytes"
	"crypto/rand"
	"testing"

	"github.com/olemoudi/kawarimi/internal/crypto"
)

func TestWrapUnwrapKey(t *testing.T) {
	wrappingKey := make([]byte, 32)
	if _, err := rand.Read(wrappingKey); err != nil {
		t.Fatal(err)
	}

	masterKey := make([]byte, 32)
	if _, err := rand.Read(masterKey); err != nil {
		t.Fatal(err)
	}

	ciphertext, nonce, err := crypto.WrapKey(wrappingKey, masterKey)
	if err != nil {
		t.Fatalf("WrapKey failed: %v", err)
	}

	if bytes.Equal(ciphertext, masterKey) {
		t.Fatal("ciphertext should not equal plaintext")
	}

	unwrapped, err := crypto.UnwrapKey(wrappingKey, ciphertext, nonce)
	if err != nil {
		t.Fatalf("UnwrapKey failed: %v", err)
	}

	if !bytes.Equal(unwrapped, masterKey) {
		t.Fatal("unwrapped key does not match original")
	}
}

func TestUnwrapKeyWrongKey(t *testing.T) {
	wrappingKey := make([]byte, 32)
	if _, err := rand.Read(wrappingKey); err != nil {
		t.Fatal(err)
	}

	masterKey := make([]byte, 32)
	if _, err := rand.Read(masterKey); err != nil {
		t.Fatal(err)
	}

	ciphertext, nonce, err := crypto.WrapKey(wrappingKey, masterKey)
	if err != nil {
		t.Fatal(err)
	}

	wrongKey := make([]byte, 32)
	if _, err := rand.Read(wrongKey); err != nil {
		t.Fatal(err)
	}

	_, err = crypto.UnwrapKey(wrongKey, ciphertext, nonce)
	if err == nil {
		t.Fatal("expected error when unwrapping with wrong key")
	}
}

func TestWrapKeyInvalidKeySize(t *testing.T) {
	_, _, err := crypto.WrapKey([]byte("short"), []byte("data"))
	if err == nil {
		t.Fatal("expected error for invalid key size")
	}
}

func TestUnwrapKeyInvalidKeySize(t *testing.T) {
	_, err := crypto.UnwrapKey([]byte("short"), []byte("data"), []byte("nonce"))
	if err == nil {
		t.Fatal("expected error for invalid key size")
	}
}

func TestWrapUnwrapEmptyPlaintext(t *testing.T) {
	wrappingKey := make([]byte, 32)
	if _, err := rand.Read(wrappingKey); err != nil {
		t.Fatal(err)
	}

	ciphertext, nonce, err := crypto.WrapKey(wrappingKey, []byte{})
	if err != nil {
		t.Fatalf("WrapKey with empty plaintext failed: %v", err)
	}

	unwrapped, err := crypto.UnwrapKey(wrappingKey, ciphertext, nonce)
	if err != nil {
		t.Fatalf("UnwrapKey with empty plaintext failed: %v", err)
	}

	if len(unwrapped) != 0 {
		t.Fatalf("expected empty plaintext, got %d bytes", len(unwrapped))
	}
}

func TestZeroBytes(t *testing.T) {
	data := []byte{1, 2, 3, 4, 5}
	crypto.ZeroBytes(data)
	for i, b := range data {
		if b != 0 {
			t.Fatalf("byte %d not zeroed: got %d", i, b)
		}
	}
}
