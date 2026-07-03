package vault_test

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/olemoudi/kawarimi/internal/crypto"
	"github.com/olemoudi/kawarimi/internal/vault"
)

func TestEnrollmentTokenRoundtrip(t *testing.T) {
	masterKey := make([]byte, 32)
	for i := range masterKey {
		masterKey[i] = byte(i)
	}

	tokenStr, code, err := vault.GenerateEnrollmentToken(masterKey)
	if err != nil {
		t.Fatalf("GenerateEnrollmentToken: %v", err)
	}
	if n := len(strings.Fields(code)); n != vault.EnrollmentCodeWords {
		t.Fatalf("expected a %d-word code, got %d: %q", vault.EnrollmentCodeWords, n, code)
	}
	if tokenStr == "" {
		t.Fatal("empty token")
	}

	recovered, err := vault.AcceptEnrollmentToken(tokenStr, code)
	if err != nil {
		t.Fatalf("AcceptEnrollmentToken: %v", err)
	}
	defer crypto.ZeroBytes(recovered)

	for i := range masterKey {
		if recovered[i] != masterKey[i] {
			t.Fatalf("master key mismatch at byte %d", i)
		}
	}
}

// TestEnrollmentTokenCodeNormalized verifies a code typed with different casing and
// spacing still works.
func TestEnrollmentTokenCodeNormalized(t *testing.T) {
	masterKey := make([]byte, 32)
	tokenStr, code, err := vault.GenerateEnrollmentToken(masterKey)
	if err != nil {
		t.Fatal(err)
	}

	messy := "  " + strings.ToUpper(code) + "  "
	recovered, err := vault.AcceptEnrollmentToken(tokenStr, messy)
	if err != nil {
		t.Fatalf("normalized code should be accepted: %v", err)
	}
	crypto.ZeroBytes(recovered)
}

func TestEnrollmentTokenWrongCode(t *testing.T) {
	masterKey := make([]byte, 32)
	tokenStr, _, _ := vault.GenerateEnrollmentToken(masterKey)

	if _, err := vault.AcceptEnrollmentToken(tokenStr, "wrong wrong wrong wrong"); err == nil {
		t.Fatal("expected error with the wrong code")
	}
}

func TestEnrollmentTokenInvalidBase64(t *testing.T) {
	if _, err := vault.AcceptEnrollmentToken("not-base64!!!", "any code words here"); err == nil {
		t.Fatal("expected error with invalid base64")
	}
}

// TestEnrollmentTokenRejectsV1 verifies the old weak (6-digit-PIN) token format is
// rejected outright rather than silently accepted.
func TestEnrollmentTokenRejectsV1(t *testing.T) {
	v1 := vault.EnrollmentToken{Version: 1, Salt: []byte{1}, Nonce: []byte{2}, Data: []byte{3}}
	j, _ := json.Marshal(v1)
	tokenStr := base64.StdEncoding.EncodeToString(j)

	_, err := vault.AcceptEnrollmentToken(tokenStr, "any four words here")
	if err == nil {
		t.Fatal("expected a v1 token to be rejected")
	}
	if !strings.Contains(err.Error(), "version") {
		t.Errorf("expected a version error, got: %v", err)
	}
}
