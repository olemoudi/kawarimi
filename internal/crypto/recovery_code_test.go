package crypto_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/olemoudi/kawarimi/internal/crypto"
)

func TestGenerateRecoveryCode(t *testing.T) {
	code, err := crypto.GenerateRecoveryCode()
	if err != nil {
		t.Fatalf("GenerateRecoveryCode failed: %v", err)
	}
	if len(code) != 16 {
		t.Fatalf("expected 16 bytes, got %d", len(code))
	}
}

func TestFormatRecoveryCode(t *testing.T) {
	code := make([]byte, 16)
	formatted := crypto.FormatRecoveryCode(code)

	// Should contain dashes
	if !strings.Contains(formatted, "-") {
		t.Fatal("formatted code should contain dashes")
	}

	// Should be uppercase base32 with dashes
	clean := strings.ReplaceAll(formatted, "-", "")
	for _, c := range clean {
		if !((c >= 'A' && c <= 'Z') || (c >= '2' && c <= '7')) {
			t.Fatalf("unexpected character in formatted code: %c", c)
		}
	}
}

func TestRecoveryCodeRoundtrip(t *testing.T) {
	code, _ := crypto.GenerateRecoveryCode()
	formatted := crypto.FormatRecoveryCode(code)

	decoded, err := crypto.DecodeRecoveryCode(formatted)
	if err != nil {
		t.Fatalf("DecodeRecoveryCode failed: %v", err)
	}

	if !bytes.Equal(code, decoded) {
		t.Fatal("roundtrip mismatch")
	}
}

func TestDecodeRecoveryCodeNoDashes(t *testing.T) {
	code, _ := crypto.GenerateRecoveryCode()
	formatted := crypto.FormatRecoveryCode(code)
	noDashes := strings.ReplaceAll(formatted, "-", "")

	decoded, err := crypto.DecodeRecoveryCode(noDashes)
	if err != nil {
		t.Fatalf("decode without dashes failed: %v", err)
	}

	if !bytes.Equal(code, decoded) {
		t.Fatal("mismatch")
	}
}

func TestDecodeRecoveryCodeCaseInsensitive(t *testing.T) {
	code, _ := crypto.GenerateRecoveryCode()
	formatted := strings.ToLower(crypto.FormatRecoveryCode(code))

	decoded, err := crypto.DecodeRecoveryCode(formatted)
	if err != nil {
		t.Fatalf("case-insensitive decode failed: %v", err)
	}

	if !bytes.Equal(code, decoded) {
		t.Fatal("mismatch")
	}
}

func TestDecodeRecoveryCodeEmpty(t *testing.T) {
	_, err := crypto.DecodeRecoveryCode("")
	if err == nil {
		t.Fatal("expected error for empty code")
	}
}

func TestDecodeRecoveryCodeInvalid(t *testing.T) {
	_, err := crypto.DecodeRecoveryCode("not-valid-base32!!!")
	if err == nil {
		t.Fatal("expected error for invalid code")
	}
}
