package vault_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/olemoudi/kawarimi/internal/crypto"
	"github.com/olemoudi/kawarimi/internal/vault"
)

// TestFullWorkflow tests the complete lifecycle: init → add → show → edit → export → verify → remove.
func TestFullWorkflow(t *testing.T) {
	dir := t.TempDir()
	passphrase := "integration-test-passphrase"

	// === INIT ===
	v, err := vault.Create(dir, passphrase)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Verify plaintext files exist
	for _, f := range []string{"README.md", "DECRYPT_INSTRUCTIONS.md"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Fatalf("missing %s: %v", f, err)
		}
	}

	// === ADD NOTE ===
	noteContent := []byte("# Bank Accounts\n\n- Chase: 1234567890\n- Savings: 0987654321\n")
	note, err := v.AddNote("Bank Accounts", noteContent, []string{"financial", "urgent"})
	if err != nil {
		t.Fatalf("AddNote: %v", err)
	}
	if note.Category != vault.CategoryNotes {
		t.Errorf("note category: got %q", note.Category)
	}

	// === ADD CREDENTIAL ===
	cred := &vault.Credential{
		Service:       "Gmail",
		URL:           "https://accounts.google.com",
		Username:      "user@gmail.com",
		Password:      "supersecret",
		TOTPSecret:    "JBSWY3DPEHPK3PXP",
		RecoveryCodes: []string{"abc-123", "def-456", "ghi-789"},
		Notes:         "Recovery email: backup@proton.me",
	}
	credEntry, err := v.AddCredential(cred, []string{"email"})
	if err != nil {
		t.Fatalf("AddCredential: %v", err)
	}

	// === ADD DOCUMENT ===
	docContent := []byte("%PDF-1.4 fake PDF content for testing")
	doc, err := v.AddDocument("Will Scan", "last-will.pdf", docContent, []string{"legal"})
	if err != nil {
		t.Fatalf("AddDocument: %v", err)
	}

	// === LIST ===
	if len(v.Manifest.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(v.Manifest.Entries))
	}

	noteEntries := v.Manifest.FindEntriesByCategory(vault.CategoryNotes)
	if len(noteEntries) != 1 {
		t.Fatalf("expected 1 note, got %d", len(noteEntries))
	}

	// === SHOW NOTE ===
	data, err := v.ShowEntry(note)
	if err != nil {
		t.Fatalf("ShowEntry note: %v", err)
	}
	if string(data) != string(noteContent) {
		t.Fatalf("note content mismatch")
	}

	// === SHOW CREDENTIAL ===
	credData, err := v.ShowEntry(credEntry)
	if err != nil {
		t.Fatalf("ShowEntry credential: %v", err)
	}
	var decoded vault.Credential
	if err := json.Unmarshal(credData, &decoded); err != nil {
		t.Fatalf("unmarshal credential: %v", err)
	}
	if decoded.Password != "supersecret" {
		t.Errorf("credential password mismatch: got %q", decoded.Password)
	}
	if decoded.TOTPSecret != "JBSWY3DPEHPK3PXP" {
		t.Errorf("TOTP mismatch: got %q", decoded.TOTPSecret)
	}
	if len(decoded.RecoveryCodes) != 3 {
		t.Errorf("expected 3 recovery codes, got %d", len(decoded.RecoveryCodes))
	}

	// === SHOW DOCUMENT ===
	docData, err := v.ShowEntry(doc)
	if err != nil {
		t.Fatalf("ShowEntry document: %v", err)
	}
	if string(docData) != string(docContent) {
		t.Fatalf("document content mismatch")
	}

	// === EDIT NOTE ===
	updatedContent := []byte("# Bank Accounts (Updated)\n\n- Chase: 1234567890\n- Savings: CLOSED\n")
	if err := v.UpdateEntry(note, updatedContent); err != nil {
		t.Fatalf("UpdateEntry: %v", err)
	}
	updated, err := v.ShowEntry(note)
	if err != nil {
		t.Fatalf("ShowEntry after update: %v", err)
	}
	if string(updated) != string(updatedContent) {
		t.Fatalf("updated content mismatch")
	}

	// === VERIFY ===
	errs := v.Verify()
	if len(errs) != 0 {
		t.Fatalf("Verify failed: %v", errs)
	}

	// === EXPORT ===
	exportDir := filepath.Join(t.TempDir(), "export")
	if err := v.Export(exportDir); err != nil {
		t.Fatalf("Export: %v", err)
	}

	// Check exported files
	indexPath := filepath.Join(exportDir, "INDEX.md")
	if _, err := os.Stat(indexPath); err != nil {
		t.Fatalf("INDEX.md missing: %v", err)
	}

	// Verify exported note is plaintext
	noteFiles, _ := filepath.Glob(filepath.Join(exportDir, "notes", "*.md"))
	if len(noteFiles) != 1 {
		t.Fatalf("expected 1 exported note, got %d", len(noteFiles))
	}
	exportedNote, _ := os.ReadFile(noteFiles[0])
	if string(exportedNote) != string(updatedContent) {
		t.Fatalf("exported note content mismatch")
	}

	// Verify exported credential is valid JSON
	credFiles, _ := filepath.Glob(filepath.Join(exportDir, "credentials", "*.json"))
	if len(credFiles) != 1 {
		t.Fatalf("expected 1 exported credential, got %d", len(credFiles))
	}

	// Verify exported document matches original
	docFiles, _ := filepath.Glob(filepath.Join(exportDir, "documents", "*.pdf"))
	if len(docFiles) != 1 {
		t.Fatalf("expected 1 exported document, got %d", len(docFiles))
	}
	exportedDoc, _ := os.ReadFile(docFiles[0])
	if string(exportedDoc) != string(docContent) {
		t.Fatalf("exported document content mismatch")
	}

	// === STANDALONE AGE DECRYPTION ===
	// Verify that an encrypted file can be decrypted with just the passphrase
	// (simulates what family would do with the age CLI)
	encryptedPath := filepath.Join(dir, note.Filename)
	decryptedByAge, err := crypto.DecryptFile(encryptedPath, passphrase)
	if err != nil {
		t.Fatalf("standalone decryption failed: %v", err)
	}
	if string(decryptedByAge) != string(updatedContent) {
		t.Fatalf("standalone decryption content mismatch")
	}

	// === REMOVE ===
	removed, err := v.RemoveEntry(doc.ID)
	if err != nil {
		t.Fatalf("RemoveEntry: %v", err)
	}
	if removed.Title != "Will Scan" {
		t.Errorf("wrong entry removed: %q", removed.Title)
	}
	if len(v.Manifest.Entries) != 2 {
		t.Fatalf("expected 2 entries after removal, got %d", len(v.Manifest.Entries))
	}

	// Verify file is deleted from disk
	docPath := filepath.Join(dir, doc.Filename)
	if _, err := os.Stat(docPath); !os.IsNotExist(err) {
		t.Error("document file should be deleted from disk")
	}

	// === PERSISTENCE ===
	// Re-open vault and verify state
	v2, err := vault.Open(dir, passphrase)
	if err != nil {
		t.Fatalf("Re-open: %v", err)
	}
	if len(v2.Manifest.Entries) != 2 {
		t.Fatalf("expected 2 entries after reopen, got %d", len(v2.Manifest.Entries))
	}

	// Verify both remaining entries decrypt correctly
	for _, entry := range v2.Manifest.Entries {
		if _, err := v2.ShowEntry(entry); err != nil {
			t.Errorf("ShowEntry %s after reopen: %v", entry.Title, err)
		}
	}
}

// TestMultipleEntriesSameCategory tests adding multiple entries in the same category.
func TestMultipleEntriesSameCategory(t *testing.T) {
	v := createTestVault(t)

	titles := []string{"Note Alpha", "Note Beta", "Note Gamma", "Note Delta"}
	for _, title := range titles {
		_, err := v.AddNote(title, []byte("content of "+title), nil)
		if err != nil {
			t.Fatalf("AddNote %q: %v", title, err)
		}
	}

	if len(v.Manifest.Entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(v.Manifest.Entries))
	}

	// Each should have a unique filename
	seen := make(map[string]bool)
	for _, e := range v.Manifest.Entries {
		if seen[e.Filename] {
			t.Errorf("duplicate filename: %s", e.Filename)
		}
		seen[e.Filename] = true
	}

	// Each should have a unique ID
	seenIDs := make(map[string]bool)
	for _, e := range v.Manifest.Entries {
		if seenIDs[e.ID] {
			t.Errorf("duplicate ID: %s", e.ID)
		}
		seenIDs[e.ID] = true
	}
}

// TestPassphraseChange tests changing the vault passphrase.
func TestPassphraseChange(t *testing.T) {
	dir := t.TempDir()
	oldPass := "old-passphrase"
	newPass := "new-passphrase"

	v, err := vault.Create(dir, oldPass)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	v.AddNote("Secret Note", []byte("very secret"), nil)
	v.AddCredential(&vault.Credential{Service: "TestSvc", Password: "pwd"}, nil)

	// Re-encrypt all files with new passphrase
	for _, entry := range v.Manifest.Entries {
		data, err := v.ShowEntry(entry)
		if err != nil {
			t.Fatalf("ShowEntry: %v", err)
		}
		filePath := filepath.Join(dir, entry.Filename)
		if err := crypto.EncryptFile(filePath, data, newPass); err != nil {
			t.Fatalf("re-encrypt: %v", err)
		}
	}

	// Re-encrypt manifest
	v.Passphrase = newPass
	if err := vault.SaveManifest(filepath.Join(dir, vault.ManifestFile), v.Manifest, newPass); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	// Old passphrase should fail
	_, err = vault.Open(dir, oldPass)
	if err == nil {
		t.Fatal("expected error opening with old passphrase")
	}

	// New passphrase should work
	v2, err := vault.Open(dir, newPass)
	if err != nil {
		t.Fatalf("Open with new passphrase: %v", err)
	}

	if len(v2.Manifest.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(v2.Manifest.Entries))
	}

	// Verify content decrypts with new passphrase
	for _, entry := range v2.Manifest.Entries {
		if _, err := v2.ShowEntry(entry); err != nil {
			t.Errorf("ShowEntry %s with new passphrase: %v", entry.Title, err)
		}
	}
}
