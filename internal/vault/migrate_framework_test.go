package vault

import (
	"os"
	"path/filepath"
	"testing"
)

// A fresh current-format vault needs no migration and opens unchanged through the
// migrating open path.
func TestNoMigrationForCurrentVault(t *testing.T) {
	dir := t.TempDir()
	res := fastHeader(t, dir)
	if _, err := CreateV2(dir, res.AgeIdentity, res.Header.AgeRecipient); err != nil {
		t.Fatal(err)
	}

	need, err := NeedsMigration(dir)
	if err != nil {
		t.Fatal(err)
	}
	if need {
		t.Error("a current-format vault should not need migration")
	}

	appDir := t.TempDir()
	v, backup, err := OpenV2Migrating(dir, res.AgeIdentity, res.Header.AgeRecipient, appDir)
	if err != nil {
		t.Fatalf("OpenV2Migrating: %v", err)
	}
	if v == nil || backup != "" {
		t.Errorf("expected a clean open with no backup, got backup=%q", backup)
	}
}

// A header claiming a newer format than this binary understands must be refused with
// a clear message, not misread (fail-safe forward).
func TestRejectsNewerHeader(t *testing.T) {
	dir := t.TempDir()
	res := fastHeader(t, dir)
	res.Header.Version = HeaderVersion + 5
	if err := SaveHeader(dir, res.Header); err != nil {
		t.Fatal(err)
	}
	// Remove the .bak the first save may have created so it can't self-heal to an old one.
	os.Remove(filepath.Join(dir, HeaderFile+".bak"))

	if _, err := LoadHeader(dir); err == nil {
		t.Fatal("expected LoadHeader to refuse a newer-format vault")
	}
}

// BackupVaultDir must copy every file before a migration touches anything.
func TestBackupVaultDir(t *testing.T) {
	dir := t.TempDir()
	res := fastHeader(t, dir)
	v, err := CreateV2(dir, res.AgeIdentity, res.Header.AgeRecipient)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.AddNote("Bank", []byte("secret"), nil); err != nil {
		t.Fatal(err)
	}

	appDir := t.TempDir()
	backup, err := BackupVaultDir(dir, appDir)
	if err != nil {
		t.Fatalf("BackupVaultDir: %v", err)
	}
	for _, f := range []string{HeaderFile, ManifestFile} {
		if _, err := os.Stat(filepath.Join(backup, f)); err != nil {
			t.Errorf("backup missing %s: %v", f, err)
		}
	}
}
