package recipient_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olemoudi/kawarimi/internal/crypto"
	"github.com/olemoudi/kawarimi/internal/recipient"
	"github.com/olemoudi/kawarimi/internal/vault"
)

// buildV4Package builds a V4 vault package zip into a fresh dir and returns that
// dir plus the DMS key (base64) and recipient passphrase needed to open it.
func buildV4Package(t *testing.T) (dir, dmsKeyB64, passphrase string) {
	t.Helper()

	vaultDir := t.TempDir()
	tp := crypto.TestParams()
	result, err := vault.NewHeader(vault.InitParams{
		Password:          "pw",
		DeviceID:          "dev",
		MnemonicKDFParams: &tp,
		OwnerKDFParams:    &tp,
	})
	if err != nil {
		t.Fatalf("NewHeader: %v", err)
	}
	defer crypto.ZeroBytes(result.MasterKey)

	if err := vault.SaveHeader(vaultDir, result.Header); err != nil {
		t.Fatalf("SaveHeader: %v", err)
	}
	v, err := vault.CreateV2(vaultDir, result.AgeIdentity, result.Header.AgeRecipient)
	if err != nil {
		t.Fatalf("CreateV2: %v", err)
	}
	if _, err := v.AddNote("Bank", []byte("# Bank\n\nAccount 123"), nil); err != nil {
		t.Fatalf("AddNote: %v", err)
	}

	dmsKey, _ := crypto.GenerateDMSKey()
	passphrase, _ = crypto.GenerateRecipientPassphrase()
	entropy, _ := crypto.DecodeMnemonic(result.MnemonicWords)
	sealed, err := crypto.SealMnemonicV4(entropy, dmsKey, passphrase)
	if err != nil {
		t.Fatalf("SealMnemonicV4: %v", err)
	}
	if err := os.WriteFile(filepath.Join(vaultDir, vault.SealedPayloadFile), sealed, 0600); err != nil {
		t.Fatalf("writing sealed payload: %v", err)
	}

	dir = t.TempDir()
	if err := vault.BuildPackage(vaultDir, filepath.Join(dir, "kawarimi-vault.zip"), ""); err != nil {
		t.Fatalf("BuildPackage: %v", err)
	}
	return dir, crypto.EncodeDMSKey(dmsKey), passphrase
}

// TestRunOpensPackageZip drives the full wizard: language menu, a key pasted with
// stray whitespace, one wrong passphrase, then the correct one (uppercased, to
// exercise normalization). It auto-detects and extracts the package zip.
func TestRunOpensPackageZip(t *testing.T) {
	dir, dmsKeyB64, passphrase := buildV4Package(t)

	input := strings.Join([]string{
		"1",                     // choose Spanish
		"  " + dmsKeyB64 + "  ", // key with stray spaces
		"abandon abandon abandon abandon abandon abandon", // wrong passphrase
		dmsKeyB64,                   // retry re-prompts the key
		strings.ToUpper(passphrase), // correct words, uppercased
	}, "\n") + "\n"

	var out bytes.Buffer
	if err := recipient.Run(recipient.Options{In: strings.NewReader(input), Out: &out, StartDir: dir}); err != nil {
		t.Fatalf("Run: %v\noutput:\n%s", err, out.String())
	}

	notes, _ := filepath.Glob(filepath.Join(dir, "decrypted", "notes", "*.md"))
	if len(notes) != 1 {
		t.Fatalf("expected 1 decrypted note, got %d\noutput:\n%s", len(notes), out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "decrypted", "INDEX.md")); err != nil {
		t.Errorf("INDEX.md not written: %v", err)
	}
	if !strings.Contains(out.String(), "Listo.") {
		t.Errorf("expected Spanish success message, got:\n%s", out.String())
	}
}

// TestRunOpensExtractedVaultDir opens an already-extracted vault directory (no zip)
// and exercises the English messages.
func TestRunOpensExtractedVaultDir(t *testing.T) {
	dir, dmsKeyB64, passphrase := buildV4Package(t)
	if _, err := vault.ExtractPackage(filepath.Join(dir, "kawarimi-vault.zip"), dir); err != nil {
		t.Fatalf("ExtractPackage: %v", err)
	}
	os.Remove(filepath.Join(dir, "kawarimi-vault.zip"))

	input := dmsKeyB64 + "\n" + passphrase + "\n"
	var out bytes.Buffer
	if err := recipient.Run(recipient.Options{In: strings.NewReader(input), Out: &out, StartDir: dir, Lang: "en"}); err != nil {
		t.Fatalf("Run: %v\noutput:\n%s", err, out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "decrypted", "INDEX.md")); err != nil {
		t.Errorf("INDEX.md not written: %v", err)
	}
	if !strings.Contains(out.String(), "Done.") {
		t.Errorf("expected English success message, got:\n%s", out.String())
	}
}

// TestRunNoVault reports a friendly error when there is nothing to open.
func TestRunNoVault(t *testing.T) {
	var out bytes.Buffer
	err := recipient.Run(recipient.Options{In: strings.NewReader(""), Out: &out, StartDir: t.TempDir(), Lang: "en"})
	if err == nil {
		t.Fatal("expected an error when no vault or package is present")
	}
	if !strings.Contains(out.String(), "Could not find a vault") {
		t.Errorf("expected the no-vault message, got:\n%s", out.String())
	}
}
