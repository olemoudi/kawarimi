package selfupdate

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// replaceBinary installs newBytes at exePath, atomically where the OS allows.
// On POSIX a rename over a running binary is fine (the process keeps the old
// inode). Windows cannot replace a running .exe, so it renames the current one
// aside first; the leftover is cleaned up on the next launch by CleanupOld.
func replaceBinary(exePath string, newBytes []byte) error {
	dir := filepath.Dir(exePath)
	tmp, err := os.CreateTemp(dir, ".kawarimi-update-*")
	if err != nil {
		return fmt.Errorf("preparing update: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(newBytes); err != nil {
		tmp.Close()
		return fmt.Errorf("writing update: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("syncing update: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing update: %w", err)
	}
	if err := os.Chmod(tmpName, 0755); err != nil {
		return fmt.Errorf("setting update permissions: %w", err)
	}

	if runtime.GOOS == "windows" {
		old := exePath + ".old"
		os.Remove(old)
		if err := os.Rename(exePath, old); err != nil {
			return fmt.Errorf("moving current binary aside: %w", err)
		}
		if err := os.Rename(tmpName, exePath); err != nil {
			os.Rename(old, exePath) // best-effort rollback
			return fmt.Errorf("installing update: %w", err)
		}
		cleanup = false
		return nil
	}

	if err := os.Rename(tmpName, exePath); err != nil {
		return fmt.Errorf("installing update: %w", err)
	}
	cleanup = false
	return nil
}

// CleanupOld removes the leftover <exe>.old from a previous Windows update. It is a
// best-effort no-op everywhere else and safe to call on every startup.
func CleanupOld() {
	if runtime.GOOS != "windows" {
		return
	}
	if exe, err := os.Executable(); err == nil {
		os.Remove(exe + ".old")
	}
}
