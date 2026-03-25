package vault

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/olemoudi/kawarimi/internal/crypto"
)

const (
	// EnrollmentTokenExpiry is how long an enrollment token is valid.
	EnrollmentTokenExpiry = 10 * time.Minute
	// PINLength is the number of digits in the enrollment PIN.
	PINLength = 6
)

// enrollmentPayload is the plaintext content of an enrollment token.
type enrollmentPayload struct {
	MasterKey []byte `json:"mk"`
	CreatedAt int64  `json:"ts"`
	Nonce     []byte `json:"n"`
}

// EnrollmentToken is the encrypted blob transferred to the new device.
type EnrollmentToken struct {
	Version int    `json:"v"`
	Salt    []byte `json:"s"`
	Nonce   []byte `json:"n"`
	Data    []byte `json:"d"`
}

// GenerateEnrollmentToken creates a PIN-protected token containing the master key.
// Returns the token (to transfer to new device) and the PIN (to communicate verbally).
func GenerateEnrollmentToken(masterKey []byte) (tokenStr string, pin string, err error) {
	// Generate PIN
	pin, err = generatePIN()
	if err != nil {
		return "", "", fmt.Errorf("generating PIN: %w", err)
	}

	// Create payload
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", "", fmt.Errorf("generating nonce: %w", err)
	}

	payload := enrollmentPayload{
		MasterKey: masterKey,
		CreatedAt: time.Now().UTC().Unix(),
		Nonce:     nonce,
	}

	plaintext, err := json.Marshal(payload)
	if err != nil {
		return "", "", fmt.Errorf("marshaling payload: %w", err)
	}
	defer crypto.ZeroBytes(plaintext)

	// Derive wrapping key from PIN
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return "", "", fmt.Errorf("generating salt: %w", err)
	}

	// Use minimum KDF params — PIN is short-lived (10 min) and only protects in-transit
	wrappingKey, err := crypto.DeriveKey([]byte(pin), salt, crypto.Argon2Params{
		Time:      1,
		MemoryKiB: 65536,
		Threads:   1,
	})
	if err != nil {
		return "", "", fmt.Errorf("deriving key: %w", err)
	}
	defer crypto.ZeroBytes(wrappingKey)

	// Encrypt payload
	ciphertext, gcmNonce, err := crypto.WrapKey(wrappingKey, plaintext)
	if err != nil {
		return "", "", fmt.Errorf("encrypting token: %w", err)
	}

	token := EnrollmentToken{
		Version: 1,
		Salt:    salt,
		Nonce:   gcmNonce,
		Data:    ciphertext,
	}

	tokenJSON, err := json.Marshal(token)
	if err != nil {
		return "", "", fmt.Errorf("marshaling token: %w", err)
	}

	return base64.StdEncoding.EncodeToString(tokenJSON), pin, nil
}

// AcceptEnrollmentToken decrypts a token using the PIN and returns the master key.
// Returns an error if the PIN is wrong or the token has expired.
func AcceptEnrollmentToken(tokenStr, pin string) ([]byte, error) {
	tokenJSON, err := base64.StdEncoding.DecodeString(tokenStr)
	if err != nil {
		return nil, fmt.Errorf("invalid token encoding: %w", err)
	}

	var token EnrollmentToken
	if err := json.Unmarshal(tokenJSON, &token); err != nil {
		return nil, fmt.Errorf("invalid token format: %w", err)
	}

	if token.Version != 1 {
		return nil, fmt.Errorf("unsupported token version: %d", token.Version)
	}

	// Derive wrapping key from PIN
	wrappingKey, err := crypto.DeriveKey([]byte(pin), token.Salt, crypto.Argon2Params{
		Time:      1,
		MemoryKiB: 65536,
		Threads:   1,
	})
	if err != nil {
		return nil, fmt.Errorf("deriving key: %w", err)
	}
	defer crypto.ZeroBytes(wrappingKey)

	// Decrypt payload
	plaintext, err := crypto.UnwrapKey(wrappingKey, token.Data, token.Nonce)
	if err != nil {
		return nil, fmt.Errorf("wrong PIN or corrupted token")
	}

	var payload enrollmentPayload
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		crypto.ZeroBytes(plaintext)
		return nil, fmt.Errorf("invalid token payload: %w", err)
	}
	crypto.ZeroBytes(plaintext)

	// Check expiry
	createdAt := time.Unix(payload.CreatedAt, 0)
	if time.Since(createdAt) > EnrollmentTokenExpiry {
		crypto.ZeroBytes(payload.MasterKey)
		return nil, fmt.Errorf("token expired (created %s ago, max %s)", time.Since(createdAt).Round(time.Second), EnrollmentTokenExpiry)
	}

	return payload.MasterKey, nil
}

func generatePIN() (string, error) {
	max := new(big.Int).Exp(big.NewInt(10), big.NewInt(PINLength), nil)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%0*d", PINLength, n), nil
}
