package crypto_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/olemoudi/kawarimi/internal/crypto"
)

func TestMain(m *testing.M) {
	// Use fast scrypt for v1 encryption tests
	crypto.ScryptWorkFactor = 10
	os.Exit(m.Run())
}

func TestEncryptDecryptRoundtrip(t *testing.T) {
	passphrase := "test-passphrase-123"
	plaintext := []byte("hello world, this is sensitive data")

	ciphertext, err := crypto.Encrypt(plaintext, passphrase)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	if bytes.Equal(ciphertext, plaintext) {
		t.Fatal("ciphertext should not equal plaintext")
	}

	decrypted, err := crypto.Decrypt(ciphertext, passphrase)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("decrypted does not match original: got %q, want %q", decrypted, plaintext)
	}
}

func TestDecryptWrongPassphrase(t *testing.T) {
	passphrase := "correct-passphrase"
	plaintext := []byte("secret data")

	ciphertext, err := crypto.Encrypt(plaintext, passphrase)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	_, err = crypto.Decrypt(ciphertext, "wrong-passphrase")
	if err == nil {
		t.Fatal("expected error when decrypting with wrong passphrase")
	}
}

func TestEncryptDecryptEmpty(t *testing.T) {
	passphrase := "test-pass"
	plaintext := []byte("")

	ciphertext, err := crypto.Encrypt(plaintext, passphrase)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	decrypted, err := crypto.Decrypt(ciphertext, passphrase)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("decrypted empty data mismatch: got %q", decrypted)
	}
}

func TestEncryptDecryptLargeData(t *testing.T) {
	passphrase := "test-pass"
	plaintext := bytes.Repeat([]byte("x"), 1024*1024) // 1MB

	ciphertext, err := crypto.Encrypt(plaintext, passphrase)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	decrypted, err := crypto.Decrypt(ciphertext, passphrase)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Fatal("decrypted large data does not match")
	}
}

func TestEncryptDecryptFile(t *testing.T) {
	tmpDir := t.TempDir()
	passphrase := "file-test-pass"
	plaintext := []byte("file content here")
	filePath := filepath.Join(tmpDir, "test.age")

	if err := crypto.EncryptFile(filePath, plaintext, passphrase); err != nil {
		t.Fatalf("EncryptFile failed: %v", err)
	}

	// File should exist and not contain plaintext
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("reading encrypted file: %v", err)
	}
	if bytes.Equal(data, plaintext) {
		t.Fatal("encrypted file should not contain plaintext")
	}

	// File permissions should be 0600
	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("expected 0600 permissions, got %o", perm)
	}

	decrypted, err := crypto.DecryptFile(filePath, passphrase)
	if err != nil {
		t.Fatalf("DecryptFile failed: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("decrypted file content mismatch: got %q, want %q", decrypted, plaintext)
	}
}

func TestDecryptFileNotFound(t *testing.T) {
	_, err := crypto.DecryptFile("/nonexistent/path.age", "pass")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
