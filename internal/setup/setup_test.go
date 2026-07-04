package setup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/olemoudi/kawarimi/internal/config"
	"github.com/olemoudi/kawarimi/internal/crypto"
	"github.com/olemoudi/kawarimi/internal/deadswitch"
	"github.com/olemoudi/kawarimi/internal/vault"
)

// testInit runs InitVault into an isolated HOME with fast KDF params and returns
// the produced secrets plus the vault and app directories.
func testInit(t *testing.T) (*InitSecrets, string, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	vaultDir := filepath.Join(home, "vault")
	fast := crypto.TestParams()

	secrets, err := InitVault(InitOptions{
		VaultDir:          vaultDir,
		Password:          "correct horse battery staple",
		DeviceID:          "testdevice",
		MnemonicKDFParams: &fast,
		OwnerKDFParams:    &fast,
	})
	if err != nil {
		t.Fatalf("InitVault: %v", err)
	}
	appDir := filepath.Join(home, config.AppDir)
	return secrets, vaultDir, appDir
}

func TestInitVaultCreatesEverything(t *testing.T) {
	secrets, vaultDir, appDir := testInit(t)

	if len(secrets.MnemonicWords) != 8 {
		t.Errorf("expected 8 mnemonic words, got %d", len(secrets.MnemonicWords))
	}
	if secrets.RecoveryCode == "" || secrets.RecipientPassphrase == "" || secrets.DMSKeyB64 == "" {
		t.Errorf("expected non-empty recovery code, passphrase, and DMS key; got %+v", secrets)
	}

	// The expected files must exist.
	for _, p := range []string{
		filepath.Join(vaultDir, vault.HeaderFile),
		filepath.Join(vaultDir, vault.SealedPayloadFile),
		filepath.Join(appDir, "device.key"),
		filepath.Join(appDir, "dms-key"),
		filepath.Join(appDir, config.ConfigFile),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected file %s to exist: %v", p, err)
		}
	}

	// Config must point at the vault dir.
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if cfg.VaultDir != vaultDir {
		t.Errorf("config VaultDir = %q, want %q", cfg.VaultDir, vaultDir)
	}
}

// TestInitVaultV4RoundTrip proves the V4 key-split works: DMS key + recipient
// passphrase together unseal and open the vault, exactly as a recipient would.
func TestInitVaultV4RoundTrip(t *testing.T) {
	secrets, vaultDir, _ := testInit(t)

	dmsKey, err := crypto.DecodeDMSKey(secrets.DMSKeyB64)
	if err != nil {
		t.Fatalf("DecodeDMSKey: %v", err)
	}
	v, err := vault.OpenSealedV4(vaultDir, dmsKey, secrets.RecipientPassphrase)
	if err != nil {
		t.Fatalf("OpenSealedV4 with DMS key + passphrase: %v", err)
	}
	if v == nil {
		t.Fatal("expected a non-nil vault")
	}

	// Neither secret alone should open it.
	if _, err := vault.OpenSealedV4(vaultDir, dmsKey, "wrong words here please no"); err == nil {
		t.Error("expected failure with the wrong passphrase")
	}
	bogus := make([]byte, len(dmsKey))
	if _, err := vault.OpenSealedV4(vaultDir, bogus, secrets.RecipientPassphrase); err == nil {
		t.Error("expected failure with the wrong DMS key")
	}
}

func TestInitVaultRefusesSecondInit(t *testing.T) {
	_, _, _ = testInit(t) // configures HOME
	// A second init in the same HOME must be refused.
	fast := crypto.TestParams()
	if _, err := InitVault(InitOptions{
		VaultDir:          filepath.Join(t.TempDir(), "vault2"),
		Password:          "another password entirely",
		MnemonicKDFParams: &fast,
		OwnerKDFParams:    &fast,
	}); err == nil {
		t.Error("expected InitVault to refuse re-initialization when a config exists")
	}
}

// A lost or corrupt config must never let a re-init overwrite an existing vault
// header (which holds the only copy of the age identity).
func TestInitVaultRefusesOverExistingHeader(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	vaultDir := filepath.Join(home, "vault")
	fast := crypto.TestParams()
	opts := InitOptions{VaultDir: vaultDir, Password: "first password", MnemonicKDFParams: &fast, OwnerKDFParams: &fast}
	if _, err := InitVault(opts); err != nil {
		t.Fatalf("first init: %v", err)
	}

	// Simulate a lost/corrupt config while the vault header survives.
	if err := os.Remove(filepath.Join(home, config.AppDir, config.ConfigFile)); err != nil {
		t.Fatalf("remove config: %v", err)
	}

	opts.Password = "second password"
	if _, err := InitVault(opts); err == nil {
		t.Fatal("init overwrote an existing vault header — data would be orphaned")
	}
}

func TestStoreSwitchPayloadForMode(t *testing.T) {
	_, _, appDir := testInit(t)

	// Cloud-only: no key stored locally.
	if err := StoreSwitchPayloadForMode(appDir, false); err != nil {
		t.Fatalf("StoreSwitchPayloadForMode(cloud-only): %v", err)
	}
	if !deadswitch.SwitchIsCloudOnly(appDir) {
		t.Error("expected cloud-only mode after StoreSwitchPayloadForMode(false)")
	}

	// Local release: the DMS key (written by InitVault) is stored in the payload.
	if err := StoreSwitchPayloadForMode(appDir, true); err != nil {
		t.Fatalf("StoreSwitchPayloadForMode(local): %v", err)
	}
	if deadswitch.SwitchIsCloudOnly(appDir) {
		t.Error("expected non-cloud-only mode after StoreSwitchPayloadForMode(true)")
	}
	if !deadswitch.IsSwitchConfigured(appDir) {
		t.Error("expected the switch payload files to exist")
	}
}
