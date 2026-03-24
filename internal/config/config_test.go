package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig("/tmp/vault")
	if cfg.VaultDir != "/tmp/vault" {
		t.Errorf("VaultDir: got %q", cfg.VaultDir)
	}
	if cfg.CheckinInterval != 7 {
		t.Errorf("CheckinInterval: got %d", cfg.CheckinInterval)
	}
}

func TestSaveAndLoad(t *testing.T) {
	// Override home directory for test
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	cfg := DefaultConfig(filepath.Join(tmpHome, "test-vault"))
	cfg.SyncTargets.GitRemote = "git@github.com:test/vault.git"

	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify file exists
	configPath := filepath.Join(tmpHome, AppDir, ConfigFile)
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("config file missing: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.VaultDir != cfg.VaultDir {
		t.Errorf("VaultDir: got %q, want %q", loaded.VaultDir, cfg.VaultDir)
	}
	if loaded.SyncTargets.GitRemote != cfg.SyncTargets.GitRemote {
		t.Errorf("GitRemote: got %q, want %q", loaded.SyncTargets.GitRemote, cfg.SyncTargets.GitRemote)
	}
}

func TestLoadMissing(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	_, err := Load()
	if err == nil {
		t.Fatal("expected error loading nonexistent config")
	}
}
