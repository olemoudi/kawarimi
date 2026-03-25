package crypto

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"strings"
)

// RecoveryCodeBytes is the length of a recovery code in bytes (128 bits).
const RecoveryCodeBytes = 16

// GenerateRecoveryCode creates a random 128-bit recovery code.
func GenerateRecoveryCode() ([]byte, error) {
	code := make([]byte, RecoveryCodeBytes)
	if _, err := rand.Read(code); err != nil {
		return nil, fmt.Errorf("generating recovery code: %w", err)
	}
	return code, nil
}

// FormatRecoveryCode encodes a recovery code as a human-readable string with dashes.
// Format: XXXXX-XXXXX-XXXXX-XXXXX-XXXXX-XXX (base32, no padding)
func FormatRecoveryCode(code []byte) string {
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(code)
	// Insert dashes every 5 characters
	var parts []string
	for i := 0; i < len(encoded); i += 5 {
		end := i + 5
		if end > len(encoded) {
			end = len(encoded)
		}
		parts = append(parts, encoded[i:end])
	}
	return strings.Join(parts, "-")
}

// DecodeRecoveryCode parses a recovery code string (with or without dashes) back to bytes.
func DecodeRecoveryCode(s string) ([]byte, error) {
	// Strip dashes, spaces, and convert to uppercase
	s = strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(s), "-", ""), " ", ""))
	if s == "" {
		return nil, fmt.Errorf("recovery code is empty")
	}

	decoded, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid recovery code format: %w", err)
	}

	if len(decoded) != RecoveryCodeBytes {
		return nil, fmt.Errorf("recovery code wrong length: expected %d bytes, got %d", RecoveryCodeBytes, len(decoded))
	}

	return decoded, nil
}
