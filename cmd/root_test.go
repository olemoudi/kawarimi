package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/olemoudi/kawarimi/internal/config"
	"github.com/olemoudi/kawarimi/internal/testenv"
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
	home := testenv.SetHome(t, t.TempDir())

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

// firstRunDecision gates auto-launching the browser setup wizard on a bare
// invocation: only a completely fresh machine qualifies.
func TestFirstRunDecision(t *testing.T) {
	cases := []struct {
		config, deviceKey, payload, want bool
	}{
		{false, false, false, true}, // fresh download → wizard
		{true, false, false, false}, // configured → help
		{false, true, false, false}, // owner device (config lost) → never clobber
		{false, false, true, false}, // recipient package nearby → recipient path
		{true, true, false, false},
	}
	for _, c := range cases {
		if got := firstRunDecision(c.config, c.deviceKey, c.payload); got != c.want {
			t.Errorf("firstRunDecision(config=%v, deviceKey=%v, payload=%v) = %v, want %v",
				c.config, c.deviceKey, c.payload, got, c.want)
		}
	}
}
