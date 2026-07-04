package vault

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

// TestSyntheticMigrationRunsRegistry proves the full auto-migration sequence with a
// synthetic v1→v2 step, before any real migration ships: backup first, then the
// registered apply, then a normal open — and the backup preserves the OLD format.
func TestSyntheticMigrationRunsRegistry(t *testing.T) {
	dir := t.TempDir()
	res := fastHeader(t, dir)
	v, err := CreateV2(dir, res.AgeIdentity, res.Header.AgeRecipient)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.AddNote("Bank", []byte("survives migration"), nil); err != nil {
		t.Fatal(err)
	}

	// Age the vault: rewrite its header as the previous format version.
	res.Header.Version = HeaderVersion - 1
	if err := SaveHeader(dir, res.Header); err != nil {
		t.Fatal(err)
	}

	// Register a synthetic step for exactly that gap.
	marker := filepath.Join(dir, "migrated-marker")
	oldRegistry := vaultMigrations
	vaultMigrations = []vaultMigration{{
		from: HeaderVersion - 1,
		to:   HeaderVersion,
		apply: func(vaultDir, ageIdentity, ageRecipient string) error {
			h, err := LoadHeader(vaultDir)
			if err != nil {
				return err
			}
			h.Version = HeaderVersion
			if err := SaveHeader(vaultDir, h); err != nil {
				return err
			}
			return os.WriteFile(marker, []byte("x"), 0600)
		},
	}}
	t.Cleanup(func() { vaultMigrations = oldRegistry })

	need, err := NeedsMigration(dir)
	if err != nil || !need {
		t.Fatalf("aged vault should need migration: need=%v err=%v", need, err)
	}

	appDir := t.TempDir()
	opened, backup, err := OpenV2Migrating(dir, res.AgeIdentity, res.Header.AgeRecipient, appDir)
	if err != nil {
		t.Fatalf("OpenV2Migrating: %v", err)
	}
	if backup == "" {
		t.Fatal("a migration must report where the backup was kept")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Error("the registered migration step did not run")
	}
	if len(opened.Manifest.Entries) != 1 {
		t.Errorf("entries after migration = %d, want 1", len(opened.Manifest.Entries))
	}

	// The header is now current, and a second open migrates nothing.
	h, err := LoadHeader(dir)
	if err != nil || h.Version != HeaderVersion {
		t.Fatalf("post-migration header version = %d err=%v", h.Version, err)
	}
	if _, backup2, err := OpenV2Migrating(dir, res.AgeIdentity, res.Header.AgeRecipient, appDir); err != nil || backup2 != "" {
		t.Errorf("second open must be a plain open: backup=%q err=%v", backup2, err)
	}

	// The backup holds the vault as it was BEFORE the migration.
	backedUp, err := os.ReadFile(filepath.Join(backup, HeaderFile))
	if err != nil {
		t.Fatalf("backup missing the header: %v", err)
	}
	if !strings.Contains(string(backedUp), `"version": `+strconv.Itoa(HeaderVersion-1)) &&
		!strings.Contains(string(backedUp), `"version":`+strconv.Itoa(HeaderVersion-1)) {
		t.Errorf("backup header should be the OLD version %d:\n%s", HeaderVersion-1, backedUp)
	}
}

// A vault older than this binary with NO registered path must fail loudly — after
// taking the backup — rather than opening a format it does not understand.
func TestMigrationMissingPathFailsSafe(t *testing.T) {
	dir := t.TempDir()
	res := fastHeader(t, dir)
	if _, err := CreateV2(dir, res.AgeIdentity, res.Header.AgeRecipient); err != nil {
		t.Fatal(err)
	}
	res.Header.Version = HeaderVersion - 1
	if err := SaveHeader(dir, res.Header); err != nil {
		t.Fatal(err)
	}

	oldRegistry := vaultMigrations
	vaultMigrations = nil
	t.Cleanup(func() { vaultMigrations = oldRegistry })

	_, backup, err := OpenV2Migrating(dir, res.AgeIdentity, res.Header.AgeRecipient, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "no migration path") {
		t.Fatalf("missing registry entry must fail loudly, got %v", err)
	}
	if backup == "" {
		t.Error("even a failed migration must have taken its backup first")
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
