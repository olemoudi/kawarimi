package crypto

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
)

// RecipientPassphraseWords is the number of words in a recipient passphrase.
// 6 words from the BIP39 list = ~66 bits entropy. Combined with age's scrypt KDF,
// this provides strong brute-force resistance for the sealed payload.
const RecipientPassphraseWords = 6

// GenerateWords returns n random space-separated words from the BIP39 word list.
func GenerateWords(n int) (string, error) {
	words := make([]string, n)
	wordListLen := big.NewInt(int64(len(bip39WordList)))

	for i := 0; i < n; i++ {
		idx, err := rand.Int(rand.Reader, wordListLen)
		if err != nil {
			return "", fmt.Errorf("generating random word index: %w", err)
		}
		words[i] = bip39WordList[idx.Int64()]
	}

	return strings.Join(words, " "), nil
}

// GenerateRecipientPassphrase creates a random 6-word passphrase from the BIP39 word list.
// Returns the passphrase as a space-separated string.
func GenerateRecipientPassphrase() (string, error) {
	return GenerateWords(RecipientPassphraseWords)
}

// NormalizeWords lowercases and collapses whitespace so a word phrase typed with
// stray spaces or capitals still matches the canonical (generated) form. The
// generators already produce this form, so normalizing a correct phrase is a no-op.
func NormalizeWords(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

// ValidateRecipientPassphrase checks that a passphrase consists of valid BIP39 words.
func ValidateRecipientPassphrase(passphrase string) error {
	words := strings.Fields(passphrase)
	if len(words) != RecipientPassphraseWords {
		return fmt.Errorf("expected %d words, got %d", RecipientPassphraseWords, len(words))
	}

	for i, word := range words {
		word = strings.ToLower(strings.TrimSpace(word))
		if _, ok := bip39WordIndex[word]; !ok {
			return fmt.Errorf("word %d (%q) is not a valid BIP39 word", i+1, word)
		}
	}

	return nil
}
