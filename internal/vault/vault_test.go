package vault_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/olemoudi/kawarimi/internal/vault"
)

const testPassphrase = "test-vault-passphrase"

func createTestVault(t *testing.T) *vault.Vault {
	t.Helper()
	dir := t.TempDir()
	v, err := vault.Create(dir, testPassphrase)
	if err != nil {
		t.Fatalf("Create vault: %v", err)
	}
	return v
}

func TestCreateVault(t *testing.T) {
	dir := t.TempDir()
	v, err := vault.Create(dir, testPassphrase)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Check directory structure
	for _, sub := range []string{"notes", "credentials", "documents"} {
		path := filepath.Join(dir, sub)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("missing directory %s: %v", sub, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%s should be a directory", sub)
		}
	}

	// Check plaintext files
	for _, f := range []string{"README.md", "DECRYPT_INSTRUCTIONS.md"} {
		path := filepath.Join(dir, f)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("missing file %s: %v", f, err)
		}
	}

	// Check encrypted manifest
	manifestPath := filepath.Join(dir, "manifest.age")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("missing manifest.age: %v", err)
	}

	// Manifest should be empty
	if len(v.Manifest.Entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(v.Manifest.Entries))
	}
}

func TestCreateVaultAlreadyExists(t *testing.T) {
	dir := t.TempDir()
	_, err := vault.Create(dir, testPassphrase)
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}

	_, err = vault.Create(dir, testPassphrase)
	if err == nil {
		t.Fatal("expected error creating vault in existing location")
	}
}

func TestOpenVault(t *testing.T) {
	dir := t.TempDir()
	_, err := vault.Create(dir, testPassphrase)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	v, err := vault.Open(dir, testPassphrase)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if v.Dir != dir {
		t.Fatalf("Dir mismatch: got %s, want %s", v.Dir, dir)
	}
}

func TestOpenVaultWrongPassphrase(t *testing.T) {
	dir := t.TempDir()
	_, err := vault.Create(dir, testPassphrase)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = vault.Open(dir, "wrong-passphrase")
	if err == nil {
		t.Fatal("expected error opening with wrong passphrase")
	}
}

func TestAddNote(t *testing.T) {
	v := createTestVault(t)

	entry, err := v.AddNote("Bank Accounts", []byte("# Bank Info\nAccount: 1234"), []string{"financial"})
	if err != nil {
		t.Fatalf("AddNote: %v", err)
	}

	if entry.Title != "Bank Accounts" {
		t.Errorf("title: got %q, want %q", entry.Title, "Bank Accounts")
	}
	if entry.Category != vault.CategoryNotes {
		t.Errorf("category: got %q, want %q", entry.Category, vault.CategoryNotes)
	}
	if len(entry.ID) == 0 {
		t.Error("ID should not be empty")
	}

	// Verify entry is in manifest
	if len(v.Manifest.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(v.Manifest.Entries))
	}

	// Verify file exists on disk
	filePath := filepath.Join(v.Dir, entry.Filename)
	if _, err := os.Stat(filePath); err != nil {
		t.Fatalf("encrypted file missing: %v", err)
	}
}

func TestAddCredential(t *testing.T) {
	v := createTestVault(t)

	cred := &vault.Credential{
		Service:       "Google",
		URL:           "https://accounts.google.com",
		Username:      "user@gmail.com",
		Password:      "secret123",
		TOTPSecret:    "JBSWY3DPEHPK3PXP",
		RecoveryCodes: []string{"abc-123", "def-456"},
		Notes:         "Recovery email: backup@example.com",
	}

	entry, err := v.AddCredential(cred, nil)
	if err != nil {
		t.Fatalf("AddCredential: %v", err)
	}

	if entry.Category != vault.CategoryCredentials {
		t.Errorf("category: got %q", entry.Category)
	}

	// Decrypt and verify content
	data, err := v.ShowEntry(entry)
	if err != nil {
		t.Fatalf("ShowEntry: %v", err)
	}

	var decoded vault.Credential
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Service != "Google" {
		t.Errorf("service: got %q", decoded.Service)
	}
	if decoded.Password != "secret123" {
		t.Errorf("password: got %q", decoded.Password)
	}
}

func TestAddDocument(t *testing.T) {
	v := createTestVault(t)

	content := []byte("%PDF-1.4 fake pdf content")
	entry, err := v.AddDocument("Will Scan", "will.pdf", content, nil)
	if err != nil {
		t.Fatalf("AddDocument: %v", err)
	}

	if entry.Category != vault.CategoryDocuments {
		t.Errorf("category: got %q", entry.Category)
	}
	if entry.ContentType != "application/pdf" {
		t.Errorf("content type: got %q", entry.ContentType)
	}

	// Decrypt and verify
	data, err := v.ShowEntry(entry)
	if err != nil {
		t.Fatalf("ShowEntry: %v", err)
	}
	if string(data) != string(content) {
		t.Fatalf("content mismatch")
	}
}

func TestShowEntry(t *testing.T) {
	v := createTestVault(t)

	original := []byte("sensitive note content")
	entry, _ := v.AddNote("Test Note", original, nil)

	data, err := v.ShowEntry(entry)
	if err != nil {
		t.Fatalf("ShowEntry: %v", err)
	}

	if string(data) != string(original) {
		t.Fatalf("content mismatch: got %q, want %q", data, original)
	}
}

func TestUpdateEntry(t *testing.T) {
	v := createTestVault(t)

	entry, _ := v.AddNote("Update Me", []byte("original"), nil)
	oldUpdatedAt := entry.UpdatedAt

	newContent := []byte("updated content")
	if err := v.UpdateEntry(entry, newContent); err != nil {
		t.Fatalf("UpdateEntry: %v", err)
	}

	data, err := v.ShowEntry(entry)
	if err != nil {
		t.Fatalf("ShowEntry after update: %v", err)
	}
	if string(data) != "updated content" {
		t.Fatalf("content not updated: got %q", data)
	}

	if entry.UpdatedAt == oldUpdatedAt {
		t.Error("UpdatedAt should have changed")
	}
}

func TestRemoveEntry(t *testing.T) {
	v := createTestVault(t)

	entry, _ := v.AddNote("To Remove", []byte("goodbye"), nil)
	filePath := filepath.Join(v.Dir, entry.Filename)

	removed, err := v.RemoveEntry(entry.ID)
	if err != nil {
		t.Fatalf("RemoveEntry: %v", err)
	}

	if removed.ID != entry.ID {
		t.Errorf("wrong entry removed")
	}

	if len(v.Manifest.Entries) != 0 {
		t.Errorf("expected 0 entries after removal, got %d", len(v.Manifest.Entries))
	}

	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Error("encrypted file should have been deleted")
	}
}

func TestFindEntry(t *testing.T) {
	v := createTestVault(t)

	entry, _ := v.AddNote("Bank Accounts", []byte("data"), nil)

	// Find by ID
	found := v.Manifest.FindEntry(entry.ID)
	if found == nil {
		t.Fatal("FindEntry by ID returned nil")
	}

	// Find by name
	found = v.Manifest.FindEntry(entry.Name)
	if found == nil {
		t.Fatal("FindEntry by name returned nil")
	}

	// Not found
	found = v.Manifest.FindEntry("nonexistent")
	if found != nil {
		t.Fatal("FindEntry should return nil for nonexistent")
	}
}

func TestVerify(t *testing.T) {
	v := createTestVault(t)

	v.AddNote("Note 1", []byte("data1"), nil)
	v.AddNote("Note 2", []byte("data2"), nil)

	errs := v.Verify()
	if len(errs) != 0 {
		t.Fatalf("expected 0 errors, got %v", errs)
	}

	// Corrupt a file
	filePath := filepath.Join(v.Dir, v.Manifest.Entries[0].Filename)
	os.WriteFile(filePath, []byte("corrupted"), 0600)

	errs = v.Verify()
	if len(errs) != 1 {
		t.Fatalf("expected 1 error after corruption, got %d", len(errs))
	}
}

func TestExport(t *testing.T) {
	v := createTestVault(t)

	v.AddNote("Test Note", []byte("# Note\nHello"), nil)
	v.AddCredential(&vault.Credential{
		Service:  "TestService",
		Username: "user",
		Password: "pass",
	}, nil)

	outputDir := filepath.Join(t.TempDir(), "export")
	if err := v.Export(outputDir); err != nil {
		t.Fatalf("Export: %v", err)
	}

	// Check INDEX.md exists
	indexPath := filepath.Join(outputDir, "INDEX.md")
	if _, err := os.Stat(indexPath); err != nil {
		t.Fatalf("INDEX.md missing: %v", err)
	}

	// Check decrypted note exists
	noteFiles, _ := filepath.Glob(filepath.Join(outputDir, "notes", "*.md"))
	if len(noteFiles) != 1 {
		t.Fatalf("expected 1 note file, got %d", len(noteFiles))
	}

	// Check decrypted credential exists
	credFiles, _ := filepath.Glob(filepath.Join(outputDir, "credentials", "*.json"))
	if len(credFiles) != 1 {
		t.Fatalf("expected 1 credential file, got %d", len(credFiles))
	}
}

func TestPersistenceAcrossOpenClose(t *testing.T) {
	dir := t.TempDir()

	// Create and add entries
	v, err := vault.Create(dir, testPassphrase)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	v.AddNote("Persistent Note", []byte("should survive"), nil)

	// Re-open
	v2, err := vault.Open(dir, testPassphrase)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if len(v2.Manifest.Entries) != 1 {
		t.Fatalf("expected 1 entry after reopen, got %d", len(v2.Manifest.Entries))
	}

	entry := v2.Manifest.Entries[0]
	if entry.Title != "Persistent Note" {
		t.Errorf("title: got %q", entry.Title)
	}

	data, err := v2.ShowEntry(entry)
	if err != nil {
		t.Fatalf("ShowEntry: %v", err)
	}
	if string(data) != "should survive" {
		t.Fatalf("content: got %q", data)
	}
}
