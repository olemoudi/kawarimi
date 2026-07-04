package recipient

import (
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
