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

// --- V4 DMS Key Split tests ---

func TestGenerateDMSKey(t *testing.T) {
	key, err := crypto.GenerateDMSKey()
	if err != nil {
		t.Fatalf("GenerateDMSKey: %v", err)
	}
	if len(key) != crypto.DMSKeyBytes {
		t.Fatalf("DMS key length %d, want %d", len(key), crypto.DMSKeyBytes)
	}
	// Verify not all zeros
	allZero := true
	for _, b := range key {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Fatal("DMS key should not be all zeros")
	}
}

func TestDMSKeyEncodeDecodeRoundtrip(t *testing.T) {
	key, err := crypto.GenerateDMSKey()
	if err != nil {
		t.Fatalf("GenerateDMSKey: %v", err)
	}
	encoded := crypto.EncodeDMSKey(key)
	decoded, err := crypto.DecodeDMSKey(encoded)
	if err != nil {
		t.Fatalf("DecodeDMSKey: %v", err)
	}
	if len(decoded) != len(key) {
		t.Fatalf("decoded length %d, want %d", len(decoded), len(key))
	}
	for i := range key {
		if decoded[i] != key[i] {
			t.Fatalf("decoded differs at byte %d", i)
		}
	}
}

func TestDecodeDMSKeyInvalid(t *testing.T) {
	// Wrong length
	_, err := crypto.DecodeDMSKey(crypto.EncodeDMSKey([]byte("short")))
	if err == nil {
		t.Fatal("expected error for wrong key length")
	}
	// Invalid base64
	_, err = crypto.DecodeDMSKey("not-valid!!!")
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
}

func TestCombinePassphrase(t *testing.T) {
	key := make([]byte, crypto.DMSKeyBytes)
	for i := range key {
		key[i] = byte(i)
	}
	combined, err := crypto.CombinePassphrase(key, "test passphrase")
	if err != nil {
		t.Fatalf("CombinePassphrase: %v", err)
	}
	// Should contain the colon separator
	if len(combined) == 0 {
		t.Fatal("combined passphrase should not be empty")
	}
	// Should contain base64(key) + ":" + passphrase
	encoded := crypto.EncodeDMSKey(key)
	expected := encoded + ":" + "test passphrase"
	if combined != expected {
		t.Fatalf("got %q, want %q", combined, expected)
	}
}

func TestCombinePassphraseValidation(t *testing.T) {
	key := make([]byte, crypto.DMSKeyBytes)
	// Empty passphrase
	_, err := crypto.CombinePassphrase(key, "")
	if err == nil {
		t.Fatal("expected error for empty passphrase")
	}
	// Wrong key length
	_, err = crypto.CombinePassphrase([]byte("short"), "passphrase")
	if err == nil {
		t.Fatal("expected error for wrong key length")
	}
}

func TestSealUnsealV4Roundtrip(t *testing.T) {
	words, entropy, err := crypto.GenerateMnemonic()
	if err != nil {
		t.Fatalf("GenerateMnemonic: %v", err)
	}

	dmsKey, err := crypto.GenerateDMSKey()
	if err != nil {
		t.Fatalf("GenerateDMSKey: %v", err)
	}

	passphrase := "test recipient passphrase"

	ciphertext, err := crypto.SealMnemonicV4(entropy, dmsKey, passphrase)
	if err != nil {
		t.Fatalf("SealMnemonicV4: %v", err)
	}

	recovered, err := crypto.UnsealMnemonicV4(ciphertext, dmsKey, passphrase)
	if err != nil {
		t.Fatalf("UnsealMnemonicV4: %v", err)
	}

	for i := range entropy {
		if recovered[i] != entropy[i] {
			t.Fatalf("recovered entropy differs at byte %d", i)
		}
	}

	// Verify mnemonic words reconstruct
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

func TestV4WrongDMSKey(t *testing.T) {
	_, entropy, err := crypto.GenerateMnemonic()
	if err != nil {
		t.Fatalf("GenerateMnemonic: %v", err)
	}

	dmsKey, err := crypto.GenerateDMSKey()
	if err != nil {
		t.Fatalf("GenerateDMSKey: %v", err)
	}
	wrongKey, err := crypto.GenerateDMSKey()
	if err != nil {
		t.Fatalf("GenerateDMSKey: %v", err)
	}

	passphrase := "correct passphrase"

	ciphertext, err := crypto.SealMnemonicV4(entropy, dmsKey, passphrase)
	if err != nil {
		t.Fatalf("SealMnemonicV4: %v", err)
	}

	_, err = crypto.UnsealMnemonicV4(ciphertext, wrongKey, passphrase)
	if err == nil {
		t.Fatal("expected error with wrong DMS key")
	}
}

func TestV4WrongPassphrase(t *testing.T) {
	_, entropy, err := crypto.GenerateMnemonic()
	if err != nil {
		t.Fatalf("GenerateMnemonic: %v", err)
	}

	dmsKey, err := crypto.GenerateDMSKey()
	if err != nil {
		t.Fatalf("GenerateDMSKey: %v", err)
	}

	ciphertext, err := crypto.SealMnemonicV4(entropy, dmsKey, "correct passphrase")
	if err != nil {
		t.Fatalf("SealMnemonicV4: %v", err)
	}

	_, err = crypto.UnsealMnemonicV4(ciphertext, dmsKey, "wrong passphrase")
	if err == nil {
		t.Fatal("expected error with wrong passphrase")
	}
}

func TestV4CannotUnsealWithV3(t *testing.T) {
	// V4-sealed ciphertext cannot be opened with just the recipient passphrase (V3 path).
	// This proves the DMS key is cryptographically required.
	_, entropy, err := crypto.GenerateMnemonic()
	if err != nil {
		t.Fatalf("GenerateMnemonic: %v", err)
	}

	dmsKey, err := crypto.GenerateDMSKey()
	if err != nil {
		t.Fatalf("GenerateDMSKey: %v", err)
	}

	passphrase := "recipient passphrase"

	ciphertext, err := crypto.SealMnemonicV4(entropy, dmsKey, passphrase)
	if err != nil {
		t.Fatalf("SealMnemonicV4: %v", err)
	}

	// Try to unseal with V3 (passphrase only, no DMS key) — must fail
	_, err = crypto.UnsealMnemonic(ciphertext, passphrase)
	if err == nil {
		t.Fatal("V4 ciphertext must NOT be decryptable with V3 (passphrase alone)")
	}
}
