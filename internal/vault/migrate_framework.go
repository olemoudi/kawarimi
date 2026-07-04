package vault

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// vaultMigration upgrades a V2+ vault from one header version to the next. Each is
// idempotent-safe and must persist the new header version. Register a new one here
// whenever HeaderVersion is bumped; the auto-migration wiring (GUI unlock, CLI open)
// then upgrades every owner's vault forward, seamlessly, with a backup kept.
type vaultMigration struct {
	from, to int
	// apply upgrades the vault in place. It receives the unlocked identity/recipient
	// so it can re-encrypt entries or the manifest if the format change requires it.
	apply func(vaultDir, ageIdentity, ageRecipient string) error
}

// vaultMigrations is the ordered registry. It is empty today (HeaderVersion == 2 is
// current and the only historical step, v1→v2, is the interactive legacy path in
// MigrateV1ToV2). Future format bumps append here.
var vaultMigrations []vaultMigration

// NeedsMigration reports whether the V2+ vault at vaultDir predates this binary's
// header version and can be upgraded. It is false for a current-format or v1
// (headerless) vault.
func NeedsMigration(vaultDir string) (bool, error) {
	if !IsV2Vault(vaultDir) {
		return false, nil // v1 uses the separate interactive MigrateV1ToV2 path
	}
	h, err := LoadHeader(vaultDir)
	if err != nil {
		return false, err
	}
	return h.Version < HeaderVersion, nil
}

// MigrateToLatest upgrades the vault to the current header version, running each
// registered step in order after taking a one-time backup. It requires the unlocked
// identity (the caller opens a slot first). Returns whether anything was migrated and
// where the backup was kept. It is a no-op (migrated=false) for a current-format vault.
func MigrateToLatest(vaultDir, ageIdentity, ageRecipient, appDir string) (migrated bool, backupPath string, err error) {
	h, err := LoadHeader(vaultDir)
	if err != nil {
		return false, "", err
	}
	if h.Version >= HeaderVersion {
		return false, "", nil
	}

	backupPath, err = BackupVaultDir(vaultDir, appDir)
	if err != nil {
		return false, "", fmt.Errorf("backing up before migration: %w", err)
	}

	for h.Version < HeaderVersion {
		m := findMigration(h.Version)
		if m == nil {
			return true, backupPath, fmt.Errorf("no migration path from vault format v%d", h.Version)
		}
		if err := m.apply(vaultDir, ageIdentity, ageRecipient); err != nil {
			return true, backupPath, fmt.Errorf("migrating v%d→v%d (backup kept at %s): %w", m.from, m.to, backupPath, err)
		}
		if h, err = LoadHeader(vaultDir); err != nil {
			return true, backupPath, err
		}
	}
	return true, backupPath, nil
}

func findMigration(from int) *vaultMigration {
	for i := range vaultMigrations {
		if vaultMigrations[i].from == from {
			return &vaultMigrations[i]
		}
	}
	return nil
}

// OpenV2Migrating opens a V2 vault, first upgrading it to the current format if it
// is older (keeping a backup). It is the open path all owner unlock flows use so a
// format bump migrates seamlessly. backupPath is non-empty only when a migration
// ran. Today no migrations are registered, so this is exactly OpenV2.
func OpenV2Migrating(vaultDir, ageIdentity, ageRecipient, appDir string) (v *Vault, backupPath string, err error) {
	if need, _ := NeedsMigration(vaultDir); need {
		migrated, backup, mErr := MigrateToLatest(vaultDir, ageIdentity, ageRecipient, appDir)
		if mErr != nil {
			return nil, backup, mErr
		}
		if migrated {
			backupPath = backup
			if h, hErr := LoadHeader(vaultDir); hErr == nil {
				ageRecipient = h.AgeRecipient
			}
		}
	}
	v, err = OpenV2(vaultDir, ageIdentity, ageRecipient)
	return v, backupPath, err
}

// BackupVaultDir copies the vault directory to ~/.kawarimi/backups/<timestamp>/vault
// so a migration (or a mistake) can always be undone. Returns the backup path.
func BackupVaultDir(vaultDir, appDir string) (string, error) {
	stamp := time.Now().UTC().Format("2006-01-02T15-04-05Z")
	dest := filepath.Join(appDir, "backups", stamp, "vault")
	if err := copyTree(vaultDir, dest); err != nil {
		return "", err
	}
	return dest, nil
}

// copyTree recursively copies src into dst (files + subdirs; skips a .git dir).
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0700)
		}
		return copyFile(path, filepath.Join(dst, rel))
	})
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
