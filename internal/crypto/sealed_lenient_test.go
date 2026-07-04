package crypto

import (
	"strings"
	"testing"
)

func TestDecodeDMSKeyLenient(t *testing.T) {
	key, err := GenerateDMSKey()
	if err != nil {
		t.Fatal(err)
	}
	canonical := EncodeDMSKey(key)

	cases := map[string]string{
		"canonical":         canonical,
		"leading space":     "   " + canonical,
		"trailing newline":  canonical + "\n",
		"indented (email)":  "     " + canonical,
		"internal spaces":   canonical[:10] + " " + canonical[10:],
		"wrapped mid-key":   canonical[:20] + "\n" + canonical[20:],
		"tabs and NBSP":     "\t" + canonical[:5] + " " + canonical[5:],
		"zero-width joined": canonical[:8] + "\u200b" + canonical[8:] + "\ufeff",
	}
	for name, input := range cases {
		got, err := DecodeDMSKeyLenient(input)
		if err != nil {
			t.Errorf("%s: unexpected error: %v", name, err)
			continue
		}
		if string(got) != string(key) {
			t.Errorf("%s: decoded key mismatch", name)
		}
	}

	if _, err := DecodeDMSKeyLenient("not a key at all"); err == nil {
		t.Error("expected an error for junk input")
	}
	if _, err := DecodeDMSKeyLenient(strings.Repeat("A", 10)); err == nil {
		t.Error("expected an error for a too-short key")
	}
}

// TestSealV4NormalizationSymmetry proves that a non-canonical passphrase on the seal
// side (as a mis-typed rekey would produce) still opens with the normalized form the
// recipient path uses — the fix for a family being permanently locked out.
func TestSealV4NormalizationSymmetry(t *testing.T) {
	entropy := make([]byte, MnemonicEntropyBytes)
	for i := range entropy {
		entropy[i] = byte(i)
	}
	dmsKey, _ := GenerateDMSKey()

	// Owner re-keys and types the card sloppily: extra caps and doubled spaces.
	sloppy := "  Correct   Horse  Battery  "
	sealed, err := SealMnemonicV4(entropy, dmsKey, sloppy)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	// Recipient types it cleanly; both are normalized, so it must still open.
	clean := "correct horse battery"
	got, err := UnsealMnemonicV4(sealed, dmsKey, clean)
	if err != nil {
		t.Fatalf("unseal with normalized passphrase: %v", err)
	}
	if string(got) != string(entropy) {
		t.Error("round-trip mismatch across normalization")
	}
}
