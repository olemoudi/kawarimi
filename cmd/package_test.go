package cmd

import (
	"os"
	"path/filepath"
	"strings"
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

// resolvePackageBinaries decides between --no-binaries, --binaries <dir>, and
// building from source; the failure modes must be loud, not silent.
func TestResolvePackageBinaries(t *testing.T) {
	restore := func() {
		packageNoBinaries, packageBinaries, packageSource = false, "", ""
	}
	restore()
	t.Cleanup(restore)

	packageNoBinaries = true
	dir, cleanup, err := resolvePackageBinaries()
	if dir != "" || cleanup != nil || err != nil {
		t.Errorf("--no-binaries: got dir %q, cleanup %t, err %v", dir, cleanup != nil, err)
	}

	// --binaries pointing at a dir with no kawarimi-* binaries must refuse.
	restore()
	packageBinaries = t.TempDir()
	if _, _, err := resolvePackageBinaries(); err == nil || !strings.Contains(err.Error(), "no kawarimi-") {
		t.Errorf("empty --binaries dir must fail loud, got %v", err)
	}

	// --binaries with real-looking binaries is passed through.
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "kawarimi-linux-amd64"), []byte("elf"), 0755); err != nil {
		t.Fatal(err)
	}
	packageBinaries = binDir
	dir, cleanup, err = resolvePackageBinaries()
	if dir != binDir || err != nil {
		t.Errorf("--binaries: got (%q, %v)", dir, err)
	}
	if cleanup != nil {
		cleanup()
	}

	// Default mode outside a source checkout: a clear error, not a broken package.
	restore()
	t.Chdir(t.TempDir())
	if _, _, err := resolvePackageBinaries(); err == nil || !strings.Contains(err.Error(), "--no-binaries") {
		t.Errorf("no source found must explain the options, got %v", err)
	}
}
