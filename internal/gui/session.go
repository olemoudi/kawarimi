package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/olemoudi/kawarimi/internal/config"
	"github.com/olemoudi/kawarimi/internal/crypto"
	"github.com/olemoudi/kawarimi/internal/vault"
)

// session holds the single owner session for the local GUI: the unlocked vault and
// the GitHub token used transiently during cloud setup. It is guarded by a mutex
// because HTTP handlers run concurrently.
type session struct {
	mu      sync.Mutex
	vault   *vault.Vault
	header  *vault.Header
	cfg     *config.Config
	ghToken string
}

func (s *session) isUnlocked() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.vault != nil
}

// withVault runs fn with the unlocked vault while holding the session lock, so
// concurrent HTTP handlers cannot mutate the manifest at the same time. Vault
// operations are not goroutine-safe on their own.
func (s *session) withVault(fn func(*vault.Vault) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.vault == nil {
		return fmt.Errorf("vault is locked")
	}
	return fn(s.vault)
}

// vaultEntryCount returns the number of entries, or 0 if locked.
func (s *session) vaultEntryCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.vault == nil {
		return 0
	}
	return len(s.vault.Manifest.Entries)
}

// unlock opens the vault via the owner slot (password + this device's key), exactly
// as the CLI does. On success the vault is held in the session.
func (s *session) unlock(password string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("no vault configured on this machine: %w", err)
	}
	header, err := vault.LoadHeader(cfg.VaultDir)
	if err != nil {
		return fmt.Errorf("loading vault header: %w", err)
	}
	appDir, err := config.AppDirPath()
	if err != nil {
		return err
	}
	dkf, err := crypto.LoadDeviceKeyFile(filepath.Join(appDir, "device.key"))
	if err != nil {
		return fmt.Errorf("no device key on this machine: %w", err)
	}
	deviceKey, err := crypto.DecryptDeviceKey(dkf, password)
	if err != nil {
		return fmt.Errorf("wrong password")
	}
	defer crypto.ZeroBytes(deviceKey)

	_, ageIdentity, err := header.OpenWithOwner(password, deviceKey)
	if err != nil {
		return fmt.Errorf("unlock failed: %w", err)
	}
	v, backup, err := vault.OpenV2Migrating(cfg.VaultDir, ageIdentity, header.AgeRecipient, appDir)
	if err != nil {
		return fmt.Errorf("opening vault: %w", err)
	}
	if backup != "" {
		fmt.Fprintf(os.Stderr, "kawarimi: vault upgraded to the latest format (backup kept at %s)\n", backup)
	}

	s.mu.Lock()
	s.vault, s.header, s.cfg = v, header, cfg
	s.mu.Unlock()
	return nil
}

// setGitHubToken stores the token in memory for the cloud-setup step.
func (s *session) setGitHubToken(tok string) {
	s.mu.Lock()
	s.ghToken = tok
	s.mu.Unlock()
}

// clearGitHubToken forgets the token; called as soon as cloud setup finishes.
func (s *session) clearGitHubToken() {
	s.mu.Lock()
	s.ghToken = ""
	s.mu.Unlock()
}
