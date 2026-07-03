package vault

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/olemoudi/kawarimi/internal/crypto"
)

const (
	// EnrollmentTokenExpiry is how long an enrollment token is valid.
	EnrollmentTokenExpiry = 10 * time.Minute
	// EnrollmentCodeWords is the number of BIP39 words in the enrollment code
	// (~44 bits — far stronger than the previous 6-digit PIN).
	EnrollmentCodeWords = 4
	// enrollmentTokenVersion is the current on-disk token format.
	enrollmentTokenVersion = 2
)

// enrollmentKDFParams protects the token. The token embeds the raw master key and
// its expiry is only enforced by an honest client, so a captured token can be
// brute-forced offline — strong Argon2 makes that expensive.
func enrollmentKDFParams() crypto.Argon2Params {
	return crypto.Argon2Params{Time: 3, MemoryKiB: 262144, Threads: 4}
}

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

// GenerateEnrollmentToken creates a code-protected token containing the master key.
// Returns the token (to transfer to the new device) and the code (4 words, to
// communicate out-of-band).
func GenerateEnrollmentToken(masterKey []byte) (tokenStr string, code string, err error) {
	code, err = crypto.GenerateWords(EnrollmentCodeWords)
	if err != nil {
		return "", "", fmt.Errorf("generating enrollment code: %w", err)
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

	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return "", "", fmt.Errorf("generating salt: %w", err)
	}

	wrappingKey, err := crypto.DeriveKey([]byte(crypto.NormalizeWords(code)), salt, enrollmentKDFParams())
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
		Version: enrollmentTokenVersion,
		Salt:    salt,
		Nonce:   gcmNonce,
		Data:    ciphertext,
	}

	tokenJSON, err := json.Marshal(token)
	if err != nil {
		return "", "", fmt.Errorf("marshaling token: %w", err)
	}

	return base64.StdEncoding.EncodeToString(tokenJSON), code, nil
}

// AcceptEnrollmentToken decrypts a token using the code and returns the master key.
// Returns an error if the code is wrong or the token has expired.
func AcceptEnrollmentToken(tokenStr, code string) ([]byte, error) {
	tokenJSON, err := base64.StdEncoding.DecodeString(tokenStr)
	if err != nil {
		return nil, fmt.Errorf("invalid token encoding: %w", err)
	}

	var token EnrollmentToken
	if err := json.Unmarshal(tokenJSON, &token); err != nil {
		return nil, fmt.Errorf("invalid token format: %w", err)
	}

	if token.Version != enrollmentTokenVersion {
		return nil, fmt.Errorf("unsupported token version %d — re-run 'kawarimi device enroll' on the trusted device to mint a new token", token.Version)
	}

	wrappingKey, err := crypto.DeriveKey([]byte(crypto.NormalizeWords(code)), token.Salt, enrollmentKDFParams())
	if err != nil {
		return nil, fmt.Errorf("deriving key: %w", err)
	}
	defer crypto.ZeroBytes(wrappingKey)

	// Decrypt payload
	plaintext, err := crypto.UnwrapKey(wrappingKey, token.Data, token.Nonce)
	if err != nil {
		return nil, fmt.Errorf("wrong code or corrupted token")
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
