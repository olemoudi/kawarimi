package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSignRoundTrip(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("RELEASE_SIGNING_KEY", base64.StdEncoding.EncodeToString(priv.Seed()))

	dir := t.TempDir()
	artifact := filepath.Join(dir, "checksums.txt")
	sigOut := filepath.Join(dir, "checksums.txt.sig")
	content := []byte("abc123  kawarimi-linux-amd64\n")
	if err := os.WriteFile(artifact, content, 0644); err != nil {
		t.Fatal(err)
	}

	if err := run(artifact, sigOut); err != nil {
		t.Fatalf("run: %v", err)
	}

	sigB64, err := os.ReadFile(sigOut)
	if err != nil {
		t.Fatal(err)
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigB64)))
	if err != nil {
		t.Fatalf("signature is not base64: %v", err)
	}
	if !ed25519.Verify(pub, content, sig) {
		t.Error("signature does not verify against the matching public key")
	}
	if ed25519.Verify(pub, append(content, 'X'), sig) {
		t.Error("signature must not verify a modified artifact")
	}
}

func TestRunFailsLoudWithoutKey(t *testing.T) {
	t.Setenv("RELEASE_SIGNING_KEY", "")
	err := run("whatever", "out.sig")
	if err == nil || !strings.Contains(err.Error(), "RELEASE_SIGNING_KEY is not set") {
		t.Fatalf("missing key must fail loud, got %v", err)
	}
}

func TestRunRejectsMalformedKey(t *testing.T) {
	for _, bad := range []string{"not base64!!", base64.StdEncoding.EncodeToString([]byte("short"))} {
		t.Setenv("RELEASE_SIGNING_KEY", bad)
		if err := run("whatever", "out.sig"); err == nil || !strings.Contains(err.Error(), "32-byte") {
			t.Errorf("key %q must be rejected, got %v", bad, err)
		}
	}
}

func TestRunReportsMissingArtifact(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	t.Setenv("RELEASE_SIGNING_KEY", base64.StdEncoding.EncodeToString(priv.Seed()))
	if err := run(filepath.Join(t.TempDir(), "nope"), "out.sig"); err == nil {
		t.Fatal("a missing artifact must fail")
	}
}
