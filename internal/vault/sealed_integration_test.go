package vault_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/olemoudi/kawarimi/internal/crypto"
	"github.com/olemoudi/kawarimi/internal/vault"
)

// TestSealedPayloadFullFlow tests the complete sealed payload lifecycle:
// init vault → seal mnemonic → build package → extract package → unseal → open vault → export
func TestSealedPayloadFullFlow(t *testing.T) {
	// === Phase 1: Create vault (simulates kawarimi init) ===
	vaultDir := t.TempDir()

	tp := crypto.TestParams()
	params := vault.InitParams{
		Password:          "test-password",
		DeviceID:          "test-device",
		MnemonicKDFParams: &tp,
		OwnerKDFParams:    &tp,
	}
	result, err := vault.NewHeader(params)
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

	// Add test content
	_, err = v.AddNote("Bank Accounts", []byte("# Bank Accounts\n\nChase: 123456\nWells Fargo: 789012"), nil)
	if err != nil {
		t.Fatalf("AddNote: %v", err)
	}

	// === Phase 2: Generate sealed payload (simulates init's sealed payload generation) ===
	recipientPassphrase, err := crypto.GenerateRecipientPassphrase()
	if err != nil {
		t.Fatalf("GenerateRecipientPassphrase: %v", err)
	}

	mnemonicEntropy, err := crypto.DecodeMnemonic(result.MnemonicWords)
	if err != nil {
		t.Fatalf("DecodeMnemonic: %v", err)
	}

	sealedPayload, err := crypto.SealMnemonic(mnemonicEntropy, recipientPassphrase)
	if err != nil {
		t.Fatalf("SealMnemonic: %v", err)
	}

	// Encode for transport (as the DMS would store/deliver it)
	sealedBase64 := crypto.EncodeSealedPayload(sealedPayload)

	// === Phase 3: Build vault package ===
	workDir := t.TempDir()
	packagePath := filepath.Join(workDir, "vault-package.zip")

	if err := vault.BuildPackage(vaultDir, packagePath, ""); err != nil {
		t.Fatalf("BuildPackage: %v", err)
	}

	// === Phase 4: Simulate recipient flow ===
	// Extract package (recipient downloads and extracts)
	extractDir := filepath.Join(workDir, "extracted")
	extractedVaultDir, err := vault.ExtractPackage(packagePath, extractDir)
	if err != nil {
		t.Fatalf("ExtractPackage: %v", err)
	}

	// Verify vault header exists
	headerPath := filepath.Join(extractedVaultDir, vault.HeaderFile)
	if _, err := os.Stat(headerPath); os.IsNotExist(err) {
		t.Fatal("vault header not found in extracted package")
	}

	// Decode sealed payload (recipient receives base64 from DMS email)
	ciphertext, err := crypto.DecodeSealedPayload(sealedBase64)
	if err != nil {
		t.Fatalf("DecodeSealedPayload: %v", err)
	}

	// Unseal mnemonic (recipient uses passphrase from physical card)
	recoveredEntropy, err := crypto.UnsealMnemonic(ciphertext, recipientPassphrase)
	if err != nil {
		t.Fatalf("UnsealMnemonic: %v", err)
	}
	defer crypto.ZeroBytes(recoveredEntropy)

	// Convert to mnemonic words
	recoveredWords, err := crypto.EncodeMnemonic(recoveredEntropy)
	if err != nil {
		t.Fatalf("EncodeMnemonic: %v", err)
	}

	// Verify mnemonic matches original
	for i, w := range result.MnemonicWords {
		if recoveredWords[i] != w {
			t.Fatalf("mnemonic word %d: got %q, want %q", i, recoveredWords[i], w)
		}
	}

	// Open vault with recovered mnemonic
	extractedHeader, err := vault.LoadHeader(extractedVaultDir)
	if err != nil {
		t.Fatalf("LoadHeader (extracted): %v", err)
	}

	_, ageIdentity, err := extractedHeader.OpenWithMnemonic(recoveredWords)
	if err != nil {
		t.Fatalf("OpenWithMnemonic: %v", err)
	}

	extractedVault, err := vault.OpenV2(extractedVaultDir, ageIdentity, extractedHeader.AgeRecipient)
	if err != nil {
		t.Fatalf("OpenV2 (extracted): %v", err)
	}

	// Export vault
	exportDir := filepath.Join(workDir, "decrypted")
	if err := extractedVault.Export(exportDir); err != nil {
		t.Fatalf("Export: %v", err)
	}

	// Verify exported content
	indexPath := filepath.Join(exportDir, "INDEX.md")
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		t.Fatal("INDEX.md not found in export")
	}

	// Verify the note was decrypted
	noteFiles, err := filepath.Glob(filepath.Join(exportDir, "notes", "*.md"))
	if err != nil {
		t.Fatalf("globbing notes: %v", err)
	}
	if len(noteFiles) != 1 {
		t.Fatalf("expected 1 note, got %d", len(noteFiles))
	}

	noteContent, err := os.ReadFile(noteFiles[0])
	if err != nil {
		t.Fatalf("reading note: %v", err)
	}
	if string(noteContent) != "# Bank Accounts\n\nChase: 123456\nWells Fargo: 789012" {
		t.Fatalf("note content mismatch: %q", noteContent)
	}
}

// TestSealedPayloadWrongPassphraseFails verifies that wrong passphrase can't unseal.
func TestSealedPayloadWrongPassphraseFails(t *testing.T) {
	tp := crypto.TestParams()
	result, err := vault.NewHeader(vault.InitParams{
		Password:          "test-password",
		DeviceID:          "test-device",
		MnemonicKDFParams: &tp,
		OwnerKDFParams:    &tp,
	})
	if err != nil {
		t.Fatalf("NewHeader: %v", err)
	}
	defer crypto.ZeroBytes(result.MasterKey)

	entropy, _ := crypto.DecodeMnemonic(result.MnemonicWords)
	sealed, _ := crypto.SealMnemonic(entropy, "correct passphrase words")

	// Try with wrong passphrase
	_, err = crypto.UnsealMnemonic(sealed, "wrong passphrase words")
	if err == nil {
		t.Fatal("expected error with wrong passphrase")
	}
}

// TestDMSOperatorCannotDecrypt verifies that having the sealed payload
// (as the DMS operator would) is insufficient to open the vault.
func TestDMSOperatorCannotDecrypt(t *testing.T) {
	vaultDir := t.TempDir()
	tp := crypto.TestParams()
	result, err := vault.NewHeader(vault.InitParams{
		Password:          "test-password",
		DeviceID:          "test-device",
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

	// Generate sealed payload
	passphrase, _ := crypto.GenerateRecipientPassphrase()
	entropy, _ := crypto.DecodeMnemonic(result.MnemonicWords)
	sealed, _ := crypto.SealMnemonic(entropy, passphrase)

	// DMS operator has the sealed ciphertext (base64-encoded)
	sealedBase64 := crypto.EncodeSealedPayload(sealed)

	// DMS operator tries to decode it (they can do this)
	ciphertext, err := crypto.DecodeSealedPayload(sealedBase64)
	if err != nil {
		t.Fatalf("DecodeSealedPayload: %v", err)
	}

	// But they can't unseal it without the passphrase
	_, err = crypto.UnsealMnemonic(ciphertext, "attacker-guess-passphrase")
	if err == nil {
		t.Fatal("DMS operator should NOT be able to unseal with a guess")
	}

	// They also can't open the vault directly (no mnemonic, no password)
	header, _ := vault.LoadHeader(vaultDir)
	_, _, err = header.OpenWithMnemonic([]string{"wrong", "wrong", "wrong", "wrong", "wrong", "wrong", "wrong", "wrong"})
	if err == nil {
		t.Fatal("should not open vault with wrong mnemonic")
	}
}

// --- V4 Integration Tests ---

// TestV4SealedPayloadFullFlow tests the complete V4 lifecycle:
// init vault → generate DMS key → seal with combined key → store in vault dir →
// build package → extract → provide DMS key + passphrase → open vault → export
func TestV4SealedPayloadFullFlow(t *testing.T) {
	// === Phase 1: Create vault (simulates kawarimi init) ===
	vaultDir := t.TempDir()

	tp := crypto.TestParams()
	params := vault.InitParams{
		Password:          "test-password",
		DeviceID:          "test-device",
		MnemonicKDFParams: &tp,
		OwnerKDFParams:    &tp,
	}
	result, err := vault.NewHeader(params)
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

	// Add test content
	_, err = v.AddNote("Bank Accounts", []byte("# Bank Accounts\n\nChase: 123456\nWells Fargo: 789012"), nil)
	if err != nil {
		t.Fatalf("AddNote: %v", err)
	}

	// === Phase 2: Generate DMS key + sealed payload (V4) ===
	dmsKey, err := crypto.GenerateDMSKey()
	if err != nil {
		t.Fatalf("GenerateDMSKey: %v", err)
	}

	recipientPassphrase, err := crypto.GenerateRecipientPassphrase()
	if err != nil {
		t.Fatalf("GenerateRecipientPassphrase: %v", err)
	}

	mnemonicEntropy, err := crypto.DecodeMnemonic(result.MnemonicWords)
	if err != nil {
		t.Fatalf("DecodeMnemonic: %v", err)
	}

	sealedPayload, err := crypto.SealMnemonicV4(mnemonicEntropy, dmsKey, recipientPassphrase)
	if err != nil {
		t.Fatalf("SealMnemonicV4: %v", err)
	}

	// Save sealed payload to vault directory (as init would do)
	sealedPath := filepath.Join(vaultDir, vault.SealedPayloadFile)
	if err := os.WriteFile(sealedPath, sealedPayload, 0600); err != nil {
		t.Fatalf("writing sealed payload: %v", err)
	}

	// === Phase 3: Build vault package ===
	workDir := t.TempDir()
	packagePath := filepath.Join(workDir, "vault-package.zip")

	if err := vault.BuildPackage(vaultDir, packagePath, ""); err != nil {
		t.Fatalf("BuildPackage: %v", err)
	}

	// === Phase 4: Simulate recipient flow ===
	extractDir := filepath.Join(workDir, "extracted")
	extractedVaultDir, err := vault.ExtractPackage(packagePath, extractDir)
	if err != nil {
		t.Fatalf("ExtractPackage: %v", err)
	}

	// Verify sealed payload exists in extracted vault
	extractedSealedPath := filepath.Join(extractedVaultDir, vault.SealedPayloadFile)
	if _, err := os.Stat(extractedSealedPath); os.IsNotExist(err) {
		t.Fatal("sealed_payload.age not found in extracted package")
	}

	// Read sealed payload from vault (recipient has this from the download)
	ciphertext, err := os.ReadFile(extractedSealedPath)
	if err != nil {
		t.Fatalf("reading extracted sealed payload: %v", err)
	}

	// Unseal with DMS key (from email) + passphrase (from card)
	recoveredEntropy, err := crypto.UnsealMnemonicV4(ciphertext, dmsKey, recipientPassphrase)
	if err != nil {
		t.Fatalf("UnsealMnemonicV4: %v", err)
	}
	defer crypto.ZeroBytes(recoveredEntropy)

	// Convert to mnemonic words
	recoveredWords, err := crypto.EncodeMnemonic(recoveredEntropy)
	if err != nil {
		t.Fatalf("EncodeMnemonic: %v", err)
	}

	// Open vault with recovered mnemonic
	extractedHeader, err := vault.LoadHeader(extractedVaultDir)
	if err != nil {
		t.Fatalf("LoadHeader (extracted): %v", err)
	}

	_, ageIdentity, err := extractedHeader.OpenWithMnemonic(recoveredWords)
	if err != nil {
		t.Fatalf("OpenWithMnemonic: %v", err)
	}

	extractedVault, err := vault.OpenV2(extractedVaultDir, ageIdentity, extractedHeader.AgeRecipient)
	if err != nil {
		t.Fatalf("OpenV2 (extracted): %v", err)
	}

	// Export and verify content
	exportDir := filepath.Join(workDir, "decrypted")
	if err := extractedVault.Export(exportDir); err != nil {
		t.Fatalf("Export: %v", err)
	}

	noteFiles, err := filepath.Glob(filepath.Join(exportDir, "notes", "*.md"))
	if err != nil {
		t.Fatalf("globbing notes: %v", err)
	}
	if len(noteFiles) != 1 {
		t.Fatalf("expected 1 note, got %d", len(noteFiles))
	}

	noteContent, err := os.ReadFile(noteFiles[0])
	if err != nil {
		t.Fatalf("reading note: %v", err)
	}
	if string(noteContent) != "# Bank Accounts\n\nChase: 123456\nWells Fargo: 789012" {
		t.Fatalf("note content mismatch: %q", noteContent)
	}
}

// TestV4VaultAloneInsufficient verifies that having the vault + sealed payload + passphrase
// (but NOT the DMS key) is insufficient to open the vault.
func TestV4VaultAloneInsufficient(t *testing.T) {
	tp := crypto.TestParams()
	result, err := vault.NewHeader(vault.InitParams{
		Password:          "test-password",
		DeviceID:          "test-device",
		MnemonicKDFParams: &tp,
		OwnerKDFParams:    &tp,
	})
	if err != nil {
		t.Fatalf("NewHeader: %v", err)
	}
	defer crypto.ZeroBytes(result.MasterKey)

	dmsKey, _ := crypto.GenerateDMSKey()
	passphrase, _ := crypto.GenerateRecipientPassphrase()
	entropy, _ := crypto.DecodeMnemonic(result.MnemonicWords)
	sealed, _ := crypto.SealMnemonicV4(entropy, dmsKey, passphrase)

	// Attacker has vault + sealed payload + passphrase, but NOT dms_key
	wrongKey, _ := crypto.GenerateDMSKey() // different key
	_, err = crypto.UnsealMnemonicV4(sealed, wrongKey, passphrase)
	if err == nil {
		t.Fatal("should not unseal with wrong DMS key")
	}

	// Attacker tries with just the passphrase (V3 path) — must fail
	_, err = crypto.UnsealMnemonic(sealed, passphrase)
	if err == nil {
		t.Fatal("V4 sealed payload must not be openable with V3 (passphrase only)")
	}
}

// TestV4DMSPlusVaultNeedPassphrase verifies that DMS key + sealed payload
// (but NOT the passphrase) is insufficient.
func TestV4DMSPlusVaultNeedPassphrase(t *testing.T) {
	tp := crypto.TestParams()
	result, err := vault.NewHeader(vault.InitParams{
		Password:          "test-password",
		DeviceID:          "test-device",
		MnemonicKDFParams: &tp,
		OwnerKDFParams:    &tp,
	})
	if err != nil {
		t.Fatalf("NewHeader: %v", err)
	}
	defer crypto.ZeroBytes(result.MasterKey)

	dmsKey, _ := crypto.GenerateDMSKey()
	passphrase, _ := crypto.GenerateRecipientPassphrase()
	entropy, _ := crypto.DecodeMnemonic(result.MnemonicWords)
	sealed, _ := crypto.SealMnemonicV4(entropy, dmsKey, passphrase)

	// Attacker has DMS key + sealed payload, but wrong passphrase
	_, err = crypto.UnsealMnemonicV4(sealed, dmsKey, "wrong passphrase words")
	if err == nil {
		t.Fatal("should not unseal with wrong passphrase")
	}
}

// TestV4RekeyRoundTrip verifies the semantics `switch rekey` relies on: resealing
// the same entropy under a new DMS key invalidates the old key while the unchanged
// card passphrase keeps working, and the recovered entropy is identical so the vault
// still opens.
func TestV4RekeyRoundTrip(t *testing.T) {
	tp := crypto.TestParams()
	result, err := vault.NewHeader(vault.InitParams{
		Password:          "test-password",
		DeviceID:          "test-device",
		MnemonicKDFParams: &tp,
		OwnerKDFParams:    &tp,
	})
	if err != nil {
		t.Fatalf("NewHeader: %v", err)
	}
	defer crypto.ZeroBytes(result.MasterKey)

	passphrase, _ := crypto.GenerateRecipientPassphrase()
	entropy, _ := crypto.DecodeMnemonic(result.MnemonicWords)

	oldKey, _ := crypto.GenerateDMSKey() // key that leaked / was disclosed
	newKey, _ := crypto.GenerateDMSKey()

	// Rekey: reseal under the new key, passphrase unchanged (cards stay valid).
	resealed, err := crypto.SealMnemonicV4(entropy, newKey, passphrase)
	if err != nil {
		t.Fatalf("reseal: %v", err)
	}

	if _, err := crypto.UnsealMnemonicV4(resealed, oldKey, passphrase); err == nil {
		t.Fatal("old DMS key must not unseal the resealed payload")
	}

	recovered, err := crypto.UnsealMnemonicV4(resealed, newKey, passphrase)
	if err != nil {
		t.Fatalf("new key + unchanged passphrase should unseal: %v", err)
	}
	if !bytes.Equal(recovered, entropy) {
		t.Fatal("recovered entropy differs after rekey — the vault would no longer open")
	}
}

// TestV4RekeyRotatePassphrase verifies `switch rekey --rotate-passphrase`: both the
// DMS key and the recipient passphrase change, so an old card no longer works.
func TestV4RekeyRotatePassphrase(t *testing.T) {
	tp := crypto.TestParams()
	result, err := vault.NewHeader(vault.InitParams{
		Password:          "test-password",
		DeviceID:          "test-device",
		MnemonicKDFParams: &tp,
		OwnerKDFParams:    &tp,
	})
	if err != nil {
		t.Fatalf("NewHeader: %v", err)
	}
	defer crypto.ZeroBytes(result.MasterKey)

	entropy, _ := crypto.DecodeMnemonic(result.MnemonicWords)
	oldPass, _ := crypto.GenerateRecipientPassphrase()
	newKey, _ := crypto.GenerateDMSKey()
	newPass, _ := crypto.GenerateRecipientPassphrase()

	resealed, err := crypto.SealMnemonicV4(entropy, newKey, newPass)
	if err != nil {
		t.Fatalf("reseal: %v", err)
	}

	if _, err := crypto.UnsealMnemonicV4(resealed, newKey, oldPass); err == nil {
		t.Fatal("old passphrase must not unseal after --rotate-passphrase")
	}

	recovered, err := crypto.UnsealMnemonicV4(resealed, newKey, newPass)
	if err != nil {
		t.Fatalf("new key + new passphrase should unseal: %v", err)
	}
	if !bytes.Equal(recovered, entropy) {
		t.Fatal("recovered entropy differs after rotate-passphrase rekey")
	}
}

// TestV3BackwardCompat verifies that V3-sealed vaults still work with the V3 path.
func TestV3BackwardCompat(t *testing.T) {
	tp := crypto.TestParams()
	result, err := vault.NewHeader(vault.InitParams{
		Password:          "test-password",
		DeviceID:          "test-device",
		MnemonicKDFParams: &tp,
		OwnerKDFParams:    &tp,
	})
	if err != nil {
		t.Fatalf("NewHeader: %v", err)
	}
	defer crypto.ZeroBytes(result.MasterKey)

	passphrase, _ := crypto.GenerateRecipientPassphrase()
	entropy, _ := crypto.DecodeMnemonic(result.MnemonicWords)

	// Seal with V3 (passphrase only, no DMS key)
	sealed, err := crypto.SealMnemonic(entropy, passphrase)
	if err != nil {
		t.Fatalf("SealMnemonic (V3): %v", err)
	}

	// Unseal with V3 — must work
	recovered, err := crypto.UnsealMnemonic(sealed, passphrase)
	if err != nil {
		t.Fatalf("UnsealMnemonic (V3): %v", err)
	}

	for i := range entropy {
		if recovered[i] != entropy[i] {
			t.Fatalf("recovered entropy differs at byte %d", i)
		}
	}
}
