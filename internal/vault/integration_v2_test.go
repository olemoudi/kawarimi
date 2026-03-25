package vault_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/olemoudi/kawarimi/internal/crypto"
	"github.com/olemoudi/kawarimi/internal/vault"
)

// TestV2FullWorkflow tests the complete v2 lifecycle: init → add → show → edit → export → verify → remove.
func TestV2FullWorkflow(t *testing.T) {
	dir := t.TempDir()

	// === INIT with header ===
	params := vault.InitParams{
		Password: "v2-test-password",
		DeviceID: "test-device",
	}
	result, err := vault.NewHeader(params)
	if err != nil {
		t.Fatalf("NewHeader: %v", err)
	}
	defer crypto.ZeroBytes(result.MasterKey)

	// Save header
	if err := vault.SaveHeader(dir, result.Header); err != nil {
		t.Fatalf("SaveHeader: %v", err)
	}

	// Create vault with identity
	v, err := vault.CreateV2(dir, result.AgeIdentity, result.Header.AgeRecipient)
	if err != nil {
		t.Fatalf("CreateV2: %v", err)
	}

	// Verify header and readme files exist
	for _, f := range []string{vault.HeaderFile, "README.md", "DECRYPT_INSTRUCTIONS.md"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Fatalf("missing %s: %v", f, err)
		}
	}

	// === ADD NOTE ===
	noteContent := []byte("# Bank Accounts\n\n- Chase: 1234567890\n")
	note, err := v.AddNote("Bank Accounts", noteContent, []string{"financial"})
	if err != nil {
		t.Fatalf("AddNote: %v", err)
	}

	// === ADD CREDENTIAL ===
	cred := &vault.Credential{
		Service:  "Gmail",
		Username: "user@gmail.com",
		Password: "supersecret",
	}
	credEntry, err := v.AddCredential(cred, []string{"email"})
	if err != nil {
		t.Fatalf("AddCredential: %v", err)
	}

	// === ADD DOCUMENT ===
	docContent := []byte("%PDF-1.4 fake PDF")
	doc, err := v.AddDocument("Will Scan", "will.pdf", docContent, nil)
	if err != nil {
		t.Fatalf("AddDocument: %v", err)
	}

	if len(v.Manifest.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(v.Manifest.Entries))
	}

	// === SHOW NOTE ===
	data, err := v.ShowEntry(note)
	if err != nil {
		t.Fatalf("ShowEntry note: %v", err)
	}
	if string(data) != string(noteContent) {
		t.Fatal("note content mismatch")
	}

	// === SHOW CREDENTIAL ===
	credData, err := v.ShowEntry(credEntry)
	if err != nil {
		t.Fatalf("ShowEntry credential: %v", err)
	}
	var decoded vault.Credential
	if err := json.Unmarshal(credData, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Password != "supersecret" {
		t.Fatalf("credential password mismatch: %q", decoded.Password)
	}

	// === EDIT NOTE ===
	updated := []byte("# Updated\n")
	if err := v.UpdateEntry(note, updated); err != nil {
		t.Fatalf("UpdateEntry: %v", err)
	}
	data, _ = v.ShowEntry(note)
	if string(data) != string(updated) {
		t.Fatal("updated content mismatch")
	}

	// === VERIFY ===
	errs := v.Verify()
	if len(errs) != 0 {
		t.Fatalf("Verify: %v", errs)
	}

	// === EXPORT ===
	exportDir := filepath.Join(t.TempDir(), "export")
	if err := v.Export(exportDir); err != nil {
		t.Fatalf("Export: %v", err)
	}
	if _, err := os.Stat(filepath.Join(exportDir, "INDEX.md")); err != nil {
		t.Fatal("INDEX.md missing")
	}

	// === REMOVE ===
	_, err = v.RemoveEntry(doc.ID)
	if err != nil {
		t.Fatalf("RemoveEntry: %v", err)
	}
	if len(v.Manifest.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(v.Manifest.Entries))
	}

	// === REOPEN with owner slot ===
	header2, err := vault.LoadHeader(dir)
	if err != nil {
		t.Fatalf("LoadHeader: %v", err)
	}

	mk2, identity2, err := header2.OpenWithOwner(params.Password, result.DeviceKey)
	if err != nil {
		t.Fatalf("OpenWithOwner: %v", err)
	}
	defer crypto.ZeroBytes(mk2)

	v2, err := vault.OpenV2(dir, identity2, header2.AgeRecipient)
	if err != nil {
		t.Fatalf("OpenV2: %v", err)
	}

	if len(v2.Manifest.Entries) != 2 {
		t.Fatalf("expected 2 entries after reopen, got %d", len(v2.Manifest.Entries))
	}

	for _, e := range v2.Manifest.Entries {
		if _, err := v2.ShowEntry(e); err != nil {
			t.Errorf("ShowEntry %s after reopen: %v", e.Title, err)
		}
	}

	// === REOPEN with mnemonic (receiver) ===
	mk3, identity3, err := header2.OpenWithMnemonic(result.MnemonicWords)
	if err != nil {
		t.Fatalf("OpenWithMnemonic: %v", err)
	}
	defer crypto.ZeroBytes(mk3)

	v3, err := vault.OpenV2(dir, identity3, header2.AgeRecipient)
	if err != nil {
		t.Fatalf("OpenV2 with mnemonic: %v", err)
	}

	for _, e := range v3.Manifest.Entries {
		if _, err := v3.ShowEntry(e); err != nil {
			t.Errorf("ShowEntry %s via mnemonic: %v", e.Title, err)
		}
	}

	// === REOPEN with recovery ===
	mk4, identity4, err := header2.OpenWithRecovery(params.Password, result.RecoveryCode)
	if err != nil {
		t.Fatalf("OpenWithRecovery: %v", err)
	}
	defer crypto.ZeroBytes(mk4)

	v4, err := vault.OpenV2(dir, identity4, header2.AgeRecipient)
	if err != nil {
		t.Fatalf("OpenV2 with recovery: %v", err)
	}

	for _, e := range v4.Manifest.Entries {
		if _, err := v4.ShowEntry(e); err != nil {
			t.Errorf("ShowEntry %s via recovery: %v", e.Title, err)
		}
	}
}

// TestV2PasswordChange tests O(1) password change in v2.
func TestV2PasswordChange(t *testing.T) {
	dir := t.TempDir()
	oldPassword := "old-pw"
	newPassword := "new-pw"

	params := vault.InitParams{Password: oldPassword, DeviceID: "dev"}
	result, _ := vault.NewHeader(params)
	defer crypto.ZeroBytes(result.MasterKey)
	vault.SaveHeader(dir, result.Header)

	v, _ := vault.CreateV2(dir, result.AgeIdentity, result.Header.AgeRecipient)
	v.AddNote("Secret", []byte("data"), nil)

	// Change password: update owner + recovery slots (O(1), no file re-encryption)
	// Decrypt recovery code first
	encRC, rcNonce, _ := result.Header.GetEncryptedRecoveryCode()
	recoveryCode, _ := crypto.UnwrapKey(result.MasterKey, encRC, rcNonce)

	result.Header.UpdateOwnerSlot("dev", newPassword, result.DeviceKey, result.MasterKey)
	result.Header.UpdateRecoverySlot(newPassword, recoveryCode, result.MasterKey)
	vault.SaveHeader(dir, result.Header)

	// Re-encrypt device key with new password
	dkf, _ := crypto.EncryptDeviceKey(result.DeviceKey, newPassword)
	_ = dkf // would save to disk in real flow

	// Old password should fail
	_, _, err := result.Header.OpenWithOwner(oldPassword, result.DeviceKey)
	if err == nil {
		t.Fatal("old password should fail")
	}

	// New password should work
	mk, identity, err := result.Header.OpenWithOwner(newPassword, result.DeviceKey)
	if err != nil {
		t.Fatalf("new password should work: %v", err)
	}
	defer crypto.ZeroBytes(mk)

	// Data should still decrypt (files untouched)
	v2, _ := vault.OpenV2(dir, identity, result.Header.AgeRecipient)
	for _, e := range v2.Manifest.Entries {
		if _, err := v2.ShowEntry(e); err != nil {
			t.Errorf("ShowEntry %s after pw change: %v", e.Title, err)
		}
	}

	// Mnemonic should still work (unchanged)
	mk2, _, err := result.Header.OpenWithMnemonic(result.MnemonicWords)
	if err != nil {
		t.Fatalf("mnemonic should still work: %v", err)
	}
	crypto.ZeroBytes(mk2)
}
