package sync_test

import (
	"os"
	"path/filepath"
	"testing"

	gosync "github.com/olemoudi/kawarimi/internal/sync"
)

func setupVaultDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello"), 0644)
	os.MkdirAll(filepath.Join(dir, "notes"), 0700)
	os.WriteFile(filepath.Join(dir, "notes", "001-test.md.age"), []byte("encrypted"), 0600)
	os.WriteFile(filepath.Join(dir, "manifest.age"), []byte("manifest-data"), 0600)
	return dir
}

func TestUSBSyncInitial(t *testing.T) {
	vaultDir := setupVaultDir(t)
	usbDir := t.TempDir()

	us := gosync.NewUSBSync(vaultDir, usbDir)
	if err := us.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Check files were copied
	for _, rel := range []string{"README.md", "manifest.age", "notes/001-test.md.age"} {
		path := filepath.Join(usbDir, rel)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("missing %s: %v", rel, err)
		}
	}

	// Check manifest was written
	manifestPath := filepath.Join(usbDir, "SYNC_MANIFEST.json")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Errorf("missing SYNC_MANIFEST.json: %v", err)
	}
}

func TestUSBSyncIncremental(t *testing.T) {
	vaultDir := setupVaultDir(t)
	usbDir := t.TempDir()

	us := gosync.NewUSBSync(vaultDir, usbDir)

	// First sync
	if err := us.Sync(); err != nil {
		t.Fatalf("first Sync: %v", err)
	}

	// Second sync (no changes) — should skip all files
	if err := us.Sync(); err != nil {
		t.Fatalf("second Sync: %v", err)
	}

	// Modify a file and sync again
	os.WriteFile(filepath.Join(vaultDir, "notes", "001-test.md.age"), []byte("modified"), 0600)
	if err := us.Sync(); err != nil {
		t.Fatalf("third Sync: %v", err)
	}

	// Verify modified content was copied
	data, err := os.ReadFile(filepath.Join(usbDir, "notes", "001-test.md.age"))
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if string(data) != "modified" {
		t.Errorf("expected 'modified', got %q", data)
	}
}

func TestUSBSyncMissingPath(t *testing.T) {
	us := gosync.NewUSBSync(t.TempDir(), "/nonexistent/path")
	err := us.Sync()
	if err == nil {
		t.Fatal("expected error for missing USB path")
	}
}

func TestUSBSyncNewFile(t *testing.T) {
	vaultDir := setupVaultDir(t)
	usbDir := t.TempDir()

	us := gosync.NewUSBSync(vaultDir, usbDir)
	if err := us.Sync(); err != nil {
		t.Fatalf("first Sync: %v", err)
	}

	// Add a new file
	os.WriteFile(filepath.Join(vaultDir, "notes", "002-new.md.age"), []byte("new file"), 0600)
	if err := us.Sync(); err != nil {
		t.Fatalf("second Sync: %v", err)
	}

	// Verify new file exists on USB
	if _, err := os.Stat(filepath.Join(usbDir, "notes", "002-new.md.age")); err != nil {
		t.Errorf("new file not synced: %v", err)
	}
}
