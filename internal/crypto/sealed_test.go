package crypto_test

import (
	"testing"

	"github.com/olemoudi/kawarimi/internal/crypto"
)

func TestSealUnsealRoundtrip(t *testing.T) {
	words, entropy, err := crypto.GenerateMnemonic()
	if err != nil {
		t.Fatalf("GenerateMnemonic: %v", err)
	}

	passphrase := "test passphrase words"

	ciphertext, err := crypto.SealMnemonic(entropy, passphrase)
	if err != nil {
		t.Fatalf("SealMnemonic: %v", err)
	}

	recovered, err := crypto.UnsealMnemonic(ciphertext, passphrase)
	if err != nil {
		t.Fatalf("UnsealMnemonic: %v", err)
	}

	// Verify entropy matches
	if len(recovered) != len(entropy) {
		t.Fatalf("recovered entropy length %d, want %d", len(recovered), len(entropy))
	}
	for i := range entropy {
		if recovered[i] != entropy[i] {
			t.Fatalf("recovered entropy differs at byte %d", i)
		}
	}

	// Verify we can reconstruct the same mnemonic words
	recoveredWords, err := crypto.EncodeMnemonic(recovered)
	if err != nil {
		t.Fatalf("EncodeMnemonic: %v", err)
	}
	for i, w := range words {
		if recoveredWords[i] != w {
			t.Fatalf("word %d: got %q, want %q", i, recoveredWords[i], w)
		}
	}
}

func TestSealUnsealWrongPassphrase(t *testing.T) {
	_, entropy, err := crypto.GenerateMnemonic()
	if err != nil {
		t.Fatalf("GenerateMnemonic: %v", err)
	}

	ciphertext, err := crypto.SealMnemonic(entropy, "correct-passphrase")
	if err != nil {
		t.Fatalf("SealMnemonic: %v", err)
	}

	_, err = crypto.UnsealMnemonic(ciphertext, "wrong-passphrase")
	if err == nil {
		t.Fatal("expected error with wrong passphrase")
	}
}

func TestSealMnemonicValidation(t *testing.T) {
	// Wrong entropy length
	_, err := crypto.SealMnemonic([]byte("short"), "passphrase")
	if err == nil {
		t.Fatal("expected error for wrong entropy length")
	}

	// Empty passphrase
	entropy := make([]byte, 11)
	_, err = crypto.SealMnemonic(entropy, "")
	if err == nil {
		t.Fatal("expected error for empty passphrase")
	}
}

func TestUnsealMnemonicValidation(t *testing.T) {
	// Empty ciphertext
	_, err := crypto.UnsealMnemonic(nil, "passphrase")
	if err == nil {
		t.Fatal("expected error for empty ciphertext")
	}

	// Empty passphrase
	_, err = crypto.UnsealMnemonic([]byte("dummy"), "")
	if err == nil {
		t.Fatal("expected error for empty passphrase")
	}
}

func TestEncodeSealedPayloadRoundtrip(t *testing.T) {
	_, entropy, err := crypto.GenerateMnemonic()
	if err != nil {
		t.Fatalf("GenerateMnemonic: %v", err)
	}

	ciphertext, err := crypto.SealMnemonic(entropy, "test-pass")
	if err != nil {
		t.Fatalf("SealMnemonic: %v", err)
	}

	// Encode to base64
	encoded := crypto.EncodeSealedPayload(ciphertext)
	if encoded == "" {
		t.Fatal("encoded payload should not be empty")
	}

	// Decode back
	decoded, err := crypto.DecodeSealedPayload(encoded)
	if err != nil {
		t.Fatalf("DecodeSealedPayload: %v", err)
	}

	if len(decoded) != len(ciphertext) {
		t.Fatalf("decoded length %d, want %d", len(decoded), len(ciphertext))
	}
	for i := range ciphertext {
		if decoded[i] != ciphertext[i] {
			t.Fatalf("decoded differs at byte %d", i)
		}
	}

	// Full roundtrip: unseal the decoded ciphertext
	recovered, err := crypto.UnsealMnemonic(decoded, "test-pass")
	if err != nil {
		t.Fatalf("UnsealMnemonic after decode: %v", err)
	}
	for i := range entropy {
		if recovered[i] != entropy[i] {
			t.Fatalf("recovered entropy differs at byte %d", i)
		}
	}
}

func TestDecodeSealedPayloadInvalid(t *testing.T) {
	_, err := crypto.DecodeSealedPayload("not-valid-base64!!!")
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
}
