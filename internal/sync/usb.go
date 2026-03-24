package sync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// USBSync handles copying the vault to a USB drive path.
type USBSync struct {
	VaultDir string
	USBPath  string
}

type usbManifest struct {
	SyncedAt string            `json:"synced_at"`
	Files    map[string]string `json:"files"` // relative path -> SHA-256 hash
}

const usbManifestFile = "SYNC_MANIFEST.json"

// NewUSBSync creates a USBSync with the given paths.
func NewUSBSync(vaultDir, usbPath string) *USBSync {
	return &USBSync{
		VaultDir: vaultDir,
		USBPath:  usbPath,
	}
}

// Sync copies changed files from the vault to the USB path.
func (u *USBSync) Sync() error {
	if _, err := os.Stat(u.USBPath); os.IsNotExist(err) {
		return fmt.Errorf("USB path does not exist: %s", u.USBPath)
	}

	// Load existing USB manifest if present
	existing := u.loadManifest()

	// Walk vault directory and build new manifest
	newManifest := &usbManifest{
		SyncedAt: time.Now().UTC().Format(time.RFC3339),
		Files:    make(map[string]string),
	}

	var copied, skipped int

	err := filepath.WalkDir(u.VaultDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip .git directory
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}

		rel, err := filepath.Rel(u.VaultDir, path)
		if err != nil {
			return err
		}

		destPath := filepath.Join(u.USBPath, rel)

		if d.IsDir() {
			return os.MkdirAll(destPath, 0700)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", rel, err)
		}

		hash := sha256sum(data)
		newManifest.Files[rel] = hash

		// Skip if unchanged
		if existing != nil {
			if existingHash, ok := existing.Files[rel]; ok && existingHash == hash {
				skipped++
				return nil
			}
		}

		if err := os.WriteFile(destPath, data, 0600); err != nil {
			return fmt.Errorf("writing %s: %w", rel, err)
		}
		copied++
		return nil
	})

	if err != nil {
		return fmt.Errorf("walking vault: %w", err)
	}

	// Write sync manifest
	manifestData, err := json.MarshalIndent(newManifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling USB manifest: %w", err)
	}
	manifestPath := filepath.Join(u.USBPath, usbManifestFile)
	if err := os.WriteFile(manifestPath, manifestData, 0600); err != nil {
		return fmt.Errorf("writing USB manifest: %w", err)
	}

	fmt.Printf("USB sync: %d files copied, %d unchanged\n", copied, skipped)
	return nil
}

func (u *USBSync) loadManifest() *usbManifest {
	path := filepath.Join(u.USBPath, usbManifestFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var m usbManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}
	return &m
}

func sha256sum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
