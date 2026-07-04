package vault

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/olemoudi/kawarimi/internal/crypto"
)

func fastHeader(t *testing.T, dir string) *InitResult {
	t.Helper()
	fast := crypto.TestParams()
	res, err := NewHeader(InitParams{
		Password: "durability-pw", DeviceID: "dev",
		MnemonicKDFParams: &fast, OwnerKDFParams: &fast,
	})
	if err != nil {
		t.Fatalf("NewHeader: %v", err)
	}
	if err := SaveHeader(dir, res.Header); err != nil {
		t.Fatalf("SaveHeader: %v", err)
	}
	return res
}

// A header truncated by a mid-write crash must be recoverable from the atomic backup.
func TestLoadHeaderSelfHealsFromBackup(t *testing.T) {
	dir := t.TempDir()
	res := fastHeader(t, dir)
	// A second save creates vault_header.json.bak holding the good header.
	if err := SaveHeader(dir, res.Header); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, HeaderFile+".bak")); err != nil {
		t.Fatalf("expected a header backup to exist: %v", err)
	}

	// Simulate a torn write of the primary header.
	if err := os.WriteFile(filepath.Join(dir, HeaderFile), []byte("{ truncated"), 0600); err != nil {
		t.Fatal(err)
	}

	h, err := LoadHeader(dir)
	if err != nil {
		t.Fatalf("LoadHeader should self-heal from backup, got: %v", err)
	}
	if h.Version != HeaderVersion {
		t.Errorf("recovered header version = %d, want %d", h.Version, HeaderVersion)
	}
	// The primary should have been restored from the backup.
	if _, err := parseHeader(mustRead(t, filepath.Join(dir, HeaderFile))); err != nil {
		t.Errorf("primary header not restored: %v", err)
	}
}

// With no valid backup, a corrupt header is a clear error, not a silent wrong-parse.
func TestLoadHeaderCorruptNoBackup(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, HeaderFile), []byte("garbage"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadHeader(dir); err == nil {
		t.Error("expected an error for a corrupt header with no backup")
	}
}

// RebuildManifest must re-index every decryptable entry after the manifest is lost.
func TestRebuildManifestRecoversEntries(t *testing.T) {
	dir := t.TempDir()
	res := fastHeader(t, dir)
	v, err := CreateV2(dir, res.AgeIdentity, res.Header.AgeRecipient)
	if err != nil {
		t.Fatalf("CreateV2: %v", err)
	}
	if _, err := v.AddNote("Bank Accounts", []byte("acct 123"), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := v.AddCredential(&Credential{Service: "Email", Username: "me", Password: "pw"}, nil); err != nil {
		t.Fatal(err)
	}

	// Lose the manifest entirely.
	if err := os.Remove(filepath.Join(dir, ManifestFile)); err != nil {
		t.Fatal(err)
	}

	_, n, err := RebuildManifest(dir, res.AgeIdentity, res.Header.AgeRecipient)
	if err != nil {
		t.Fatalf("RebuildManifest: %v", err)
	}
	if n != 2 {
		t.Fatalf("recovered %d entries, want 2", n)
	}

	// The rebuilt vault opens and every entry still decrypts.
	v2, err := OpenV2(dir, res.AgeIdentity, res.Header.AgeRecipient)
	if err != nil {
		t.Fatalf("OpenV2 after rebuild: %v", err)
	}
	if len(v2.Manifest.Entries) != 2 {
		t.Fatalf("rebuilt manifest has %d entries, want 2", len(v2.Manifest.Entries))
	}
	for _, e := range v2.Manifest.Entries {
		if _, err := v2.ShowEntry(e); err != nil {
			t.Errorf("entry %s not decryptable after rebuild: %v", e.Name, err)
		}
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
