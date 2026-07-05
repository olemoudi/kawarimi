package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/olemoudi/kawarimi/internal/crypto"
	"github.com/olemoudi/kawarimi/internal/testenv"
	"github.com/olemoudi/kawarimi/internal/vault"
)

// Enrolling a new device must mint a fresh device key + owner slot so the new
// machine opens the vault with ITS password — and the original slot keeps working.
func TestEnrollNewDeviceAddsWorkingSlot(t *testing.T) {
	env := testenv.New(t)
	secrets := env.InitVault(t)
	cfg := env.Config(t)

	header, err := vault.LoadHeader(env.VaultDir)
	if err != nil {
		t.Fatal(err)
	}
	masterKey, _, err := header.OpenWithMnemonic(secrets.MnemonicWords)
	if err != nil {
		t.Fatal(err)
	}
	defer crypto.ZeroBytes(masterKey)

	// Simulate the fresh machine: no device key yet (same isolated HOME).
	deviceKeyPath := filepath.Join(env.AppDir, "device.key")
	if err := os.Remove(deviceKeyPath); err != nil {
		t.Fatal(err)
	}

	const devicePassword = "second-device-password-9"
	if err := enrollNewDevice(cfg, header, masterKey, devicePassword); err != nil {
		t.Fatalf("enrollNewDevice: %v", err)
	}

	if _, err := os.Stat(deviceKeyPath); err != nil {
		t.Fatalf("enroll must write the device key: %v", err)
	}

	// The new device password opens the vault through the standard CLI path.
	withStdin(t, devicePassword+"\n")
	if _, err := openVault(); err != nil {
		t.Fatalf("the enrolled device's password must open the vault: %v", err)
	}

	// And the original password no longer matches this machine's device key.
	withStdin(t, env.Password()+"\n")
	if _, err := openVault(); err == nil {
		t.Error("the old password must not open the vault with the new device key")
	}
}
