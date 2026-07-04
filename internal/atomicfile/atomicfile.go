// Package atomicfile writes files durably: to a temp file in the same directory,
// fsync'd, then renamed over the target (an atomic replace on POSIX and Windows).
// A crash, power loss, or full disk therefore leaves either the previous complete
// file or the new complete file on disk — never a truncated one. This matters
// because kawarimi's irreplaceable state (the vault header holds every key slot and
// the wrapped identity) is rewritten in place; a torn plain os.WriteFile would brick
// the vault permanently.
package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteFile atomically writes data to path with the given permissions.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	// Remove the temp file unless the rename below claims it.
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("writing temp file: %w", err)
	}
	// Flush contents to stable storage before the rename, so the rename cannot
	// expose a file whose data hasn't landed yet.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("syncing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return fmt.Errorf("setting permissions: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replacing target: %w", err)
	}
	tmpName = "" // the rename consumed the temp file
	syncDir(dir)
	return nil
}

// WriteFileBackup is WriteFile plus a copy of the current file to path+".bak"
// (written atomically) before it is replaced, so a later torn write — or a manual
// mistake — can be recovered from the backup. A backup failure does not abort the
// main write; the atomic replace already guarantees no torn primary file.
func WriteFileBackup(path string, data []byte, perm os.FileMode) error {
	if old, err := os.ReadFile(path); err == nil {
		_ = WriteFile(path+".bak", old, perm)
	}
	return WriteFile(path, data, perm)
}

// syncDir best-effort fsyncs the directory so the rename itself is durable. Not all
// platforms support directory fsync (Windows does not); failures are ignored.
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	defer d.Close()
	_ = d.Sync()
}
