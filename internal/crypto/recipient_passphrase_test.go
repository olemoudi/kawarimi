package crypto_test

import (
	"strings"
	"testing"

	"github.com/olemoudi/kawarimi/internal/crypto"
)

func TestGenerateRecipientPassphrase(t *testing.T) {
	passphrase, err := crypto.GenerateRecipientPassphrase()
	if err != nil {
		t.Fatalf("GenerateRecipientPassphrase: %v", err)
	}

	words := strings.Fields(passphrase)
	if len(words) != crypto.RecipientPassphraseWords {
		t.Fatalf("expected %d words, got %d: %q", crypto.RecipientPassphraseWords, len(words), passphrase)
	}

	// All words should be valid
	if err := crypto.ValidateRecipientPassphrase(passphrase); err != nil {
		t.Fatalf("generated passphrase failed validation: %v", err)
	}
}

func TestGenerateRecipientPassphraseUniqueness(t *testing.T) {
	// Generate two passphrases; they should (almost certainly) be different
	p1, err := crypto.GenerateRecipientPassphrase()
	if err != nil {
		t.Fatalf("GenerateRecipientPassphrase: %v", err)
	}
	p2, err := crypto.GenerateRecipientPassphrase()
	if err != nil {
		t.Fatalf("GenerateRecipientPassphrase: %v", err)
	}

	if p1 == p2 {
		t.Fatal("two generated passphrases should not be identical")
	}
}

func TestValidateRecipientPassphrase(t *testing.T) {
	tests := []struct {
		name       string
		passphrase string
		wantErr    bool
	}{
		{"valid", "abandon ability able about above absent", false},
		{"too few words", "abandon ability able", true},
		{"too many words", "abandon ability able about above absent absorb", true},
		{"invalid word", "abandon ability able about above notaword", true},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := crypto.ValidateRecipientPassphrase(tt.passphrase)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateRecipientPassphrase(%q) error = %v, wantErr = %v", tt.passphrase, err, tt.wantErr)
			}
		})
	}
}

func TestRecipientPassphraseCanSealUnseal(t *testing.T) {
	// End-to-end: generate passphrase, use it to seal/unseal mnemonic
	passphrase, err := crypto.GenerateRecipientPassphrase()
	if err != nil {
		t.Fatalf("GenerateRecipientPassphrase: %v", err)
	}

	_, entropy, err := crypto.GenerateMnemonic()
	if err != nil {
		t.Fatalf("GenerateMnemonic: %v", err)
	}

	ciphertext, err := crypto.SealMnemonic(entropy, passphrase)
	if err != nil {
		t.Fatalf("SealMnemonic: %v", err)
	}

	recovered, err := crypto.UnsealMnemonic(ciphertext, passphrase)
	if err != nil {
		t.Fatalf("UnsealMnemonic: %v", err)
	}

	for i := range entropy {
		if recovered[i] != entropy[i] {
			t.Fatalf("recovered entropy differs at byte %d", i)
		}
	}
}
