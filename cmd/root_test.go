package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/olemoudi/kawarimi/internal/config"
	"github.com/olemoudi/kawarimi/internal/vault"
)

func TestHasNearbySealedPayload(t *testing.T) {
	// A sealed payload under cwd/vault/ (extracted package) is detected.
	cwd := t.TempDir()
	vaultDir := filepath.Join(cwd, vault.PackageVaultDir)
	if err := os.MkdirAll(vaultDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vaultDir, vault.SealedPayloadFile), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if !hasNearbySealedPayload(cwd, "") {
		t.Error("expected sealed payload under cwd/vault to be detected")
	}

	// A sealed payload directly in the executable's dir is detected.
	exeDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(exeDir, vault.SealedPayloadFile), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if !hasNearbySealedPayload(t.TempDir(), exeDir) {
		t.Error("expected sealed payload in the exe dir to be detected")
	}

	// Nothing nearby -> not detected.
	if hasNearbySealedPayload(t.TempDir(), t.TempDir()) {
		t.Error("expected no detection in empty dirs")
	}
}

func TestOwnerDeviceKeyExists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if ownerDeviceKeyExists() {
		t.Error("no device key should exist on a fresh home")
	}

	appDir := filepath.Join(home, config.AppDir)
	if err := os.MkdirAll(appDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "device.key"), []byte("k"), 0600); err != nil {
		t.Fatal(err)
	}
	if !ownerDeviceKeyExists() {
		t.Error("device key should be detected once present")
	}
}
