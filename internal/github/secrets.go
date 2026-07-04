package github

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	"golang.org/x/crypto/nacl/box"
)

// publicKey is a repository's Actions secrets public key (used to seal secrets).
type publicKey struct {
	KeyID string `json:"key_id"`
	Key   string `json:"key"` // base64-encoded 32-byte X25519 public key
}

// actionsPublicKey fetches the repo's Actions secrets public key.
func (c *Client) actionsPublicKey(ctx context.Context, owner, repo string) (publicKey, error) {
	resp, err := c.do(ctx, http.MethodGet, "/repos/"+owner+"/"+repo+"/actions/secrets/public-key", nil)
	if err != nil {
		return publicKey{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return publicKey{}, apiError(resp, "get actions public key")
	}
	var pk publicKey
	if err := json.NewDecoder(resp.Body).Decode(&pk); err != nil {
		return publicKey{}, fmt.Errorf("decoding public key: %w", err)
	}
	if pk.Key == "" || pk.KeyID == "" {
		return publicKey{}, fmt.Errorf("get actions public key: incomplete response")
	}
	return pk, nil
}

// sealSecret encrypts value for the given base64 X25519 public key using a
// libsodium sealed box (crypto_box_seal), which is what GitHub Actions requires.
func sealSecret(publicKeyB64, value string) (string, error) {
	pkBytes, err := base64.StdEncoding.DecodeString(publicKeyB64)
	if err != nil {
		return "", fmt.Errorf("decoding public key: %w", err)
	}
	if len(pkBytes) != 32 {
		return "", fmt.Errorf("unexpected public key length %d (want 32)", len(pkBytes))
	}
	var recipient [32]byte
	copy(recipient[:], pkBytes)

	sealed, err := box.SealAnonymous(nil, []byte(value), &recipient, rand.Reader)
	if err != nil {
		return "", fmt.Errorf("sealing secret: %w", err)
	}
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// SetActionsSecret creates or updates a single repository Actions secret.
func (c *Client) SetActionsSecret(ctx context.Context, owner, repo, name, value string) error {
	pk, err := c.actionsPublicKey(ctx, owner, repo)
	if err != nil {
		return err
	}
	encrypted, err := sealSecret(pk.Key, value)
	if err != nil {
		return err
	}
	resp, err := c.do(ctx, http.MethodPut, "/repos/"+owner+"/"+repo+"/actions/secrets/"+name, map[string]any{
		"encrypted_value": encrypted,
		"key_id":          pk.KeyID,
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// GitHub returns 201 (created) or 204 (updated).
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		return apiError(resp, "set secret "+name)
	}
	return nil
}

// SetActionsSecrets sets multiple secrets. It fetches the public key once and
// reuses it for every secret. Names are applied in sorted order for determinism.
func (c *Client) SetActionsSecrets(ctx context.Context, owner, repo string, kv map[string]string) error {
	if len(kv) == 0 {
		return nil
	}
	pk, err := c.actionsPublicKey(ctx, owner, repo)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(kv))
	for name := range kv {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		encrypted, err := sealSecret(pk.Key, kv[name])
		if err != nil {
			return err
		}
		resp, err := c.do(ctx, http.MethodPut, "/repos/"+owner+"/"+repo+"/actions/secrets/"+name, map[string]any{
			"encrypted_value": encrypted,
			"key_id":          pk.KeyID,
		})
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
			err := apiError(resp, "set secret "+name)
			resp.Body.Close()
			return err
		}
		resp.Body.Close()
	}
	return nil
}
