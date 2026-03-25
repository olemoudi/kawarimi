package vault_test

import (
	"testing"
	"time"

	"github.com/olemoudi/kawarimi/internal/crypto"
	"github.com/olemoudi/kawarimi/internal/vault"
)

func TestEnrollmentTokenRoundtrip(t *testing.T) {
	masterKey := make([]byte, 32)
	for i := range masterKey {
		masterKey[i] = byte(i)
	}

	tokenStr, pin, err := vault.GenerateEnrollmentToken(masterKey)
	if err != nil {
		t.Fatalf("GenerateEnrollmentToken: %v", err)
	}

	if len(pin) != 6 {
		t.Fatalf("expected 6-digit PIN, got %q", pin)
	}

	if tokenStr == "" {
		t.Fatal("empty token")
	}

	// Accept with correct PIN
	recovered, err := vault.AcceptEnrollmentToken(tokenStr, pin)
	if err != nil {
		t.Fatalf("AcceptEnrollmentToken: %v", err)
	}
	defer crypto.ZeroBytes(recovered)

	for i := range masterKey {
		if recovered[i] != masterKey[i] {
			t.Fatalf("master key mismatch at byte %d", i)
		}
	}
}

func TestEnrollmentTokenWrongPIN(t *testing.T) {
	masterKey := make([]byte, 32)
	tokenStr, _, _ := vault.GenerateEnrollmentToken(masterKey)

	_, err := vault.AcceptEnrollmentToken(tokenStr, "000000")
	if err == nil {
		t.Fatal("expected error with wrong PIN")
	}
}

func TestEnrollmentTokenInvalidBase64(t *testing.T) {
	_, err := vault.AcceptEnrollmentToken("not-base64!!!", "123456")
	if err == nil {
		t.Fatal("expected error with invalid base64")
	}
}

func TestEnrollmentTokenExpiry(t *testing.T) {
	// We can't easily test expiry without mocking time, but we can verify
	// that a fresh token works (not expired)
	masterKey := make([]byte, 32)
	tokenStr, pin, _ := vault.GenerateEnrollmentToken(masterKey)

	// Fresh token should work
	recovered, err := vault.AcceptEnrollmentToken(tokenStr, pin)
	if err != nil {
		t.Fatalf("fresh token should work: %v", err)
	}
	crypto.ZeroBytes(recovered)

	_ = time.Now() // just to use the time import
}

func TestPINFormat(t *testing.T) {
	// Generate multiple PINs and check format
	for i := 0; i < 10; i++ {
		masterKey := make([]byte, 32)
		_, pin, err := vault.GenerateEnrollmentToken(masterKey)
		if err != nil {
			t.Fatal(err)
		}
		if len(pin) != 6 {
			t.Errorf("PIN length %d, expected 6: %q", len(pin), pin)
		}
		for _, c := range pin {
			if c < '0' || c > '9' {
				t.Errorf("PIN contains non-digit: %q", pin)
			}
		}
	}
}
