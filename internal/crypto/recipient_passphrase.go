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

// GenerateRecipientPassphrase creates a random 6-word passphrase from the BIP39 word list.
// Returns the passphrase as a space-separated string.
func GenerateRecipientPassphrase() (string, error) {
	words := make([]string, RecipientPassphraseWords)
	wordListLen := big.NewInt(int64(len(bip39WordList)))

	for i := 0; i < RecipientPassphraseWords; i++ {
		idx, err := rand.Int(rand.Reader, wordListLen)
		if err != nil {
			return "", fmt.Errorf("generating random word index: %w", err)
		}
		words[i] = bip39WordList[idx.Int64()]
	}

	return strings.Join(words, " "), nil
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
