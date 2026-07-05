package cmd

import (
	"archive/zip"
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

// configExists / nearbyPayloadExists feed the first-run auto-launch decision.
func TestConfigExists(t *testing.T) {
	env := testenv.New(t)
	if configExists() {
		t.Fatal("fresh HOME must have no config")
	}
	env.InitVault(t)
	if !configExists() {
		t.Fatal("configExists must be true after init")
	}
}

func TestNearbyPayloadExists(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if nearbyPayloadExists() {
		t.Fatal("empty cwd must have no nearby payload")
	}
	if err := os.WriteFile(filepath.Join(dir, vault.SealedPayloadFile), []byte("age"), 0600); err != nil {
		t.Fatal(err)
	}
	if !nearbyPayloadExists() {
		t.Fatal("a sealed payload in cwd must be detected")
	}
}

// An UNEXTRACTED package zip next to the binary must count as a nearby payload —
// otherwise a recipient double-click falls through to the owner setup wizard.
func TestNearbyPayloadExistsWithPackageZip(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// An unrelated zip must not trigger recipient mode.
	writeTestZip(t, filepath.Join(dir, "holiday-photos.zip"), "photos/img.jpg")
	if nearbyPayloadExists() {
		t.Fatal("an unrelated zip must not count as a package")
	}

	writeTestZip(t, filepath.Join(dir, "kawarimi-vault.zip"), "vault/sealed_payload.age")
	if !nearbyPayloadExists() {
		t.Fatal("an unextracted package zip in cwd must be detected")
	}
}

// writeTestZip creates a zip at path with the given (empty) entries.
func writeTestZip(t *testing.T, path string, entries ...string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := zip.NewWriter(f)
	for _, name := range entries {
		if _, err := w.Create(name); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}
