package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olemoudi/kawarimi/internal/testenv"
)

// The recipient self-check must prove TODAY what the recipient will need to do
// post-mortem: unseal with the local DMS key + the card passphrase.
func TestVerifyRecipientPathPasses(t *testing.T) {
	env := testenv.New(t)
	secrets := env.InitVault(t)

	withStdin(t, secrets.RecipientPassphrase+"\n")
	if err := verifyRecipientPath(); err != nil {
		t.Fatalf("recipient self-check must pass right after init: %v", err)
	}
}

func TestVerifyRecipientPathWrongPassphraseFails(t *testing.T) {
	env := testenv.New(t)
	env.InitVault(t)

	withStdin(t, "abandon abandon abandon abandon abandon abandon\n")
	err := verifyRecipientPath()
	if err == nil {
		t.Fatal("a wrong card passphrase must fail the self-check")
	}
	if !strings.Contains(err.Error(), "SELF-CHECK FAILED") {
		t.Errorf("failure must be unmistakable, got: %v", err)
	}
}

// In cloud-only mode the local DMS key is deleted; the self-check must explain
// how to proceed instead of failing cryptically.
func TestVerifyRecipientPathNeedsLocalDMSKey(t *testing.T) {
	env := testenv.New(t)
	env.InitVault(t)
	if err := os.Remove(filepath.Join(env.AppDir, "dms-key")); err != nil {
		t.Fatal(err)
	}

	err := verifyRecipientPath()
	if err == nil {
		t.Fatal("missing DMS key must fail the self-check")
	}
	if !strings.Contains(err.Error(), "switch rekey") {
		t.Errorf("error must point at 'switch rekey', got: %v", err)
	}
}
