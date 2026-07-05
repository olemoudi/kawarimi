package sync

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gossh "golang.org/x/crypto/ssh"
)

// sshAuth backs every cloud push (check-ins, seeding). It must load a real
// ed25519 key and fail with the key path in the message when it cannot.
func TestSSHAuth(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-key")
	g := NewGitSync(t.TempDir(), "git@github.com:owner/repo.git", missing)
	if _, err := g.authFor(); err == nil || !strings.Contains(err.Error(), missing) {
		t.Fatalf("missing key must fail with the path in the message, got %v", err)
	}

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := gossh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(block), 0600); err != nil {
		t.Fatal(err)
	}

	g = NewGitSync(t.TempDir(), "git@github.com:owner/repo.git", keyPath)
	auth, err := g.sshAuth()
	if err != nil {
		t.Fatalf("sshAuth with a valid key: %v", err)
	}
	if auth.User != "git" {
		t.Errorf("auth user = %q, want git", auth.User)
	}

	// Local remotes need no auth at all.
	g = NewGitSync(t.TempDir(), t.TempDir(), keyPath)
	if a, err := g.authFor(); a != nil || err != nil {
		t.Errorf("local remote must need no auth, got (%v, %v)", a, err)
	}
}
