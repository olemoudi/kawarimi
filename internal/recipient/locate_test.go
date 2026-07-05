package recipient

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/olemoudi/kawarimi/internal/vault"
)

// locateVault must search every candidate dir (cwd AND the executable's dir), so a
// macOS double-click with cwd=$HOME still finds a package next to the binary.
func TestLocateVaultSearchesAllDirs(t *testing.T) {
	empty := t.TempDir()
	withVault := t.TempDir()
	if err := os.WriteFile(filepath.Join(withVault, vault.HeaderFile), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}

	dir, base, cleanup, err := locateVault([]string{empty, withVault})
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		t.Fatalf("locateVault: %v", err)
	}
	if dir != withVault || base != withVault {
		t.Errorf("dir=%q base=%q, want both %q", dir, base, withVault)
	}
}

func TestLocateVaultNotFound(t *testing.T) {
	if _, _, _, err := locateVault([]string{t.TempDir(), ""}); err == nil {
		t.Error("expected an error when no dir has a vault")
	}
}

// A decoy zip that sorts before the real package must not derail the wizard: only
// content-verified kawarimi packages are considered.
func TestLocateVaultSkipsNonPackageZip(t *testing.T) {
	dir := t.TempDir()
	writeLocateZip(t, filepath.Join(dir, "aaa-decoy.zip"), "photos/img.jpg")
	writeLocateZip(t, filepath.Join(dir, "zzz-package.zip"),
		"vault/"+vault.HeaderFile, "vault/sealed_payload.age")

	got, base, cleanup, err := locateVault([]string{dir})
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		t.Fatalf("locateVault: %v", err)
	}
	if base != dir {
		t.Errorf("base = %q, want %q", base, dir)
	}
	if _, serr := os.Stat(filepath.Join(got, vault.HeaderFile)); serr != nil {
		t.Errorf("extracted vault dir %q has no header: %v", got, serr)
	}
}

func writeLocateZip(t *testing.T, path string, entries ...string) {
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
