package crypto

import (
	"crypto/rand"
	"fmt"
	"strings"
)

const (
	// MnemonicWordCount is the number of words in a mnemonic (88 bits / 11 bits per word = 8 words).
	MnemonicWordCount = 8
	// MnemonicEntropyBytes is the number of entropy bytes (88 bits = 11 bytes).
	MnemonicEntropyBytes = 11
	// BitsPerWord is the number of bits each BIP39 word encodes.
	BitsPerWord = 11
)

// GenerateMnemonic generates a new random 8-word mnemonic and returns the words and raw entropy.
func GenerateMnemonic() (words []string, entropy []byte, err error) {
	entropy = make([]byte, MnemonicEntropyBytes)
	if _, err := rand.Read(entropy); err != nil {
		return nil, nil, fmt.Errorf("generating entropy: %w", err)
	}

	words, err = EncodeMnemonic(entropy)
	if err != nil {
		return nil, nil, err
	}

	return words, entropy, nil
}

// EncodeMnemonic encodes 11 bytes (88 bits) of entropy into 8 BIP39 words.
// Each word maps to 11 bits, so 8 words = 88 bits.
func EncodeMnemonic(entropy []byte) ([]string, error) {
	if len(entropy) != MnemonicEntropyBytes {
		return nil, fmt.Errorf("entropy must be %d bytes, got %d", MnemonicEntropyBytes, len(entropy))
	}

	// Convert entropy bytes to a bit stream and extract 11-bit indices
	words := make([]string, MnemonicWordCount)
	for i := 0; i < MnemonicWordCount; i++ {
		idx := extractBits(entropy, i*BitsPerWord, BitsPerWord)
		if idx >= len(bip39WordList) {
			return nil, fmt.Errorf("word index %d out of range", idx)
		}
		words[i] = bip39WordList[idx]
	}

	return words, nil
}

// DecodeMnemonic decodes 8 BIP39 words back into 11 bytes (88 bits) of entropy.
func DecodeMnemonic(words []string) ([]byte, error) {
	if len(words) != MnemonicWordCount {
		return nil, fmt.Errorf("expected %d words, got %d", MnemonicWordCount, len(words))
	}

	// Look up each word's index
	indices := make([]int, MnemonicWordCount)
	for i, word := range words {
		word = strings.TrimSpace(strings.ToLower(word))
		idx, ok := bip39WordIndex[word]
		if !ok {
			return nil, fmt.Errorf("unknown mnemonic word: %q", word)
		}
		indices[i] = idx
	}

	// Pack 8 x 11-bit indices into 88 bits (11 bytes)
	entropy := make([]byte, MnemonicEntropyBytes)
	for i, idx := range indices {
		setBits(entropy, i*BitsPerWord, BitsPerWord, idx)
	}

	return entropy, nil
}

// extractBits extracts `count` bits starting at bit offset `start` from a byte slice.
// Returns the extracted bits as an integer.
func extractBits(data []byte, start, count int) int {
	value := 0
	for i := 0; i < count; i++ {
		byteIdx := (start + i) / 8
		bitIdx := 7 - ((start + i) % 8)
		if byteIdx < len(data) && (data[byteIdx]>>uint(bitIdx))&1 == 1 {
			value |= 1 << uint(count-1-i)
		}
	}
	return value
}

// setBits sets `count` bits at bit offset `start` in a byte slice from an integer value.
func setBits(data []byte, start, count, value int) {
	for i := 0; i < count; i++ {
		if (value>>(count-1-i))&1 == 1 {
			byteIdx := (start + i) / 8
			bitIdx := 7 - ((start + i) % 8)
			if byteIdx < len(data) {
				data[byteIdx] |= 1 << uint(bitIdx)
			}
		}
	}
}

// bip39WordIndex maps words to their BIP39 index for fast lookup.
var bip39WordIndex map[string]int

func init() {
	bip39WordIndex = make(map[string]int, len(bip39WordList))
	for i, word := range bip39WordList {
		bip39WordIndex[word] = i
	}
}
