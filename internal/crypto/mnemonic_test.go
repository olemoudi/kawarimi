package crypto_test

import (
	"bytes"
	"testing"

	"github.com/olemoudi/kawarimi/internal/crypto"
)

func TestGenerateMnemonic(t *testing.T) {
	words, entropy, err := crypto.GenerateMnemonic()
	if err != nil {
		t.Fatalf("GenerateMnemonic failed: %v", err)
	}

	if len(words) != 8 {
		t.Fatalf("expected 8 words, got %d", len(words))
	}

	if len(entropy) != 11 {
		t.Fatalf("expected 11 bytes entropy, got %d", len(entropy))
	}

	// All words should be non-empty
	for i, w := range words {
		if w == "" {
			t.Fatalf("word %d is empty", i)
		}
	}
}

func TestMnemonicRoundtrip(t *testing.T) {
	words, entropy, err := crypto.GenerateMnemonic()
	if err != nil {
		t.Fatal(err)
	}

	// Decode back to entropy
	decoded, err := crypto.DecodeMnemonic(words)
	if err != nil {
		t.Fatalf("DecodeMnemonic failed: %v", err)
	}

	if !bytes.Equal(entropy, decoded) {
		t.Fatalf("entropy mismatch: got %x, want %x", decoded, entropy)
	}

	// Re-encode and verify same words
	reencoded, err := crypto.EncodeMnemonic(decoded)
	if err != nil {
		t.Fatal(err)
	}

	for i, w := range words {
		if w != reencoded[i] {
			t.Fatalf("word %d mismatch: got %q, want %q", i, reencoded[i], w)
		}
	}
}

func TestMnemonicKnownVector(t *testing.T) {
	// All zeros should produce the first 8 words (each 11-bit index = 0)
	entropy := make([]byte, 11)
	words, err := crypto.EncodeMnemonic(entropy)
	if err != nil {
		t.Fatal(err)
	}

	// All-zero bits means all indices are 0 → first word repeated
	for i, w := range words {
		if w != "abandon" {
			t.Fatalf("word %d: expected 'abandon' for zero entropy, got %q", i, w)
		}
	}
}

func TestMnemonicMaxVector(t *testing.T) {
	// All 0xFF bytes: each 11-bit group = 0x7FF = 2047 → last word "zoo"
	entropy := bytes.Repeat([]byte{0xFF}, 11)
	words, err := crypto.EncodeMnemonic(entropy)
	if err != nil {
		t.Fatal(err)
	}

	for i, w := range words {
		if w != "zoo" {
			t.Fatalf("word %d: expected 'zoo' for max entropy, got %q", i, w)
		}
	}
}

func TestDecodeMnemonicUnknownWord(t *testing.T) {
	words := []string{"abandon", "ability", "able", "about", "above", "absent", "absorb", "notaword"}
	_, err := crypto.DecodeMnemonic(words)
	if err == nil {
		t.Fatal("expected error for unknown word")
	}
}

func TestDecodeMnemonicWrongCount(t *testing.T) {
	_, err := crypto.DecodeMnemonic([]string{"abandon", "ability", "able"})
	if err == nil {
		t.Fatal("expected error for wrong word count")
	}
}

func TestEncodeMnemonicWrongEntropySize(t *testing.T) {
	_, err := crypto.EncodeMnemonic([]byte{1, 2, 3})
	if err == nil {
		t.Fatal("expected error for wrong entropy size")
	}
}

func TestMnemonicCaseInsensitive(t *testing.T) {
	words := []string{"ABANDON", "Ability", "aBLE", "About", "ABOVE", "absent", "ABSORB", "abstract"}
	_, err := crypto.DecodeMnemonic(words)
	if err != nil {
		t.Fatalf("case-insensitive decode failed: %v", err)
	}
}

func TestMnemonicUniqueness(t *testing.T) {
	// Generate two mnemonics and verify they differ (probabilistically)
	words1, _, err := crypto.GenerateMnemonic()
	if err != nil {
		t.Fatal(err)
	}

	words2, _, err := crypto.GenerateMnemonic()
	if err != nil {
		t.Fatal(err)
	}

	same := true
	for i := range words1 {
		if words1[i] != words2[i] {
			same = false
			break
		}
	}
	if same {
		t.Fatal("two random mnemonics should differ")
	}
}

func TestBIP39WordListSize(t *testing.T) {
	// Verify the word list init ran and has the expected size
	// We test this indirectly: encode all-zero should give "abandon" (index 0)
	// and all-max should give "zoo" (index 2047)
	entropy := make([]byte, 11)
	words, _ := crypto.EncodeMnemonic(entropy)
	if words[0] != "abandon" {
		t.Fatalf("first word in BIP39 list should be 'abandon', got %q", words[0])
	}
}
