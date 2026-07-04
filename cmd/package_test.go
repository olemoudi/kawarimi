package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKawarimiBinariesIn(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"kawarimi-linux-amd64", "kawarimi-windows-amd64.exe", "other-tool"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if got := kawarimiBinariesIn(dir); len(got) != 2 {
		t.Errorf("expected 2 kawarimi binaries, got %d: %v", len(got), got)
	}
	if got := kawarimiBinariesIn(filepath.Join(dir, "missing")); got != nil {
		t.Errorf("expected nil for missing dir, got %v", got)
	}
}

func TestIsKawarimiModule(t *testing.T) {
	yes := t.TempDir()
	if err := os.WriteFile(filepath.Join(yes, "go.mod"), []byte("module github.com/olemoudi/kawarimi\n\ngo 1.25\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if !isKawarimiModule(yes) {
		t.Error("expected the kawarimi module to be detected")
	}

	no := t.TempDir()
	if err := os.WriteFile(filepath.Join(no, "go.mod"), []byte("module example.com/other\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if isKawarimiModule(no) {
		t.Error("a different module should not be detected as kawarimi")
	}
	if isKawarimiModule(t.TempDir()) {
		t.Error("a dir with no go.mod should not be detected")
	}
}

func TestResolveSourceDirExplicit(t *testing.T) {
	if got := resolveSourceDir("/explicit/path"); got != "/explicit/path" {
		t.Errorf("explicit source = %q, want /explicit/path", got)
	}
}
