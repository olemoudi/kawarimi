package atomicfile

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteFileReplacesAndLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "state.json")

	if err := WriteFile(p, []byte("first"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if got, _ := os.ReadFile(p); string(got) != "first" {
		t.Errorf("content = %q, want first", got)
	}
	if err := WriteFile(p, []byte("second"), 0600); err != nil {
		t.Fatalf("WriteFile overwrite: %v", err)
	}
	if got, _ := os.ReadFile(p); string(got) != "second" {
		t.Errorf("content = %q, want second", got)
	}
	// The atomic write must not leave temp files behind.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("expected exactly the target file, got %v", entries)
	}
	if runtime.GOOS != "windows" { // Windows does not preserve POSIX modes
		if info, _ := os.Stat(p); info.Mode().Perm() != 0600 {
			t.Errorf("perm = %v, want 0600", info.Mode().Perm())
		}
	}
}

func TestWriteFileBackupKeepsPrevious(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "header.json")

	// No prior file → no backup, no error.
	if err := WriteFileBackup(p, []byte("v1"), 0600); err != nil {
		t.Fatalf("first WriteFileBackup: %v", err)
	}
	if _, err := os.Stat(p + ".bak"); err == nil {
		t.Error("no backup should exist after the first write")
	}

	// Second write backs up the previous content.
	if err := WriteFileBackup(p, []byte("v2"), 0600); err != nil {
		t.Fatalf("second WriteFileBackup: %v", err)
	}
	if got, _ := os.ReadFile(p); string(got) != "v2" {
		t.Errorf("primary = %q, want v2", got)
	}
	if got, _ := os.ReadFile(p + ".bak"); string(got) != "v1" {
		t.Errorf("backup = %q, want v1", got)
	}
}
