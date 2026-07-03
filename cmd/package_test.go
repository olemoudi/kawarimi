package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildTargetBinaryName(t *testing.T) {
	cases := []struct {
		t    buildTarget
		want string
	}{
		{buildTarget{"linux", "amd64", ""}, "kawarimi-linux-amd64"},
		{buildTarget{"linux", "arm64", ""}, "kawarimi-linux-arm64"},
		{buildTarget{"darwin", "amd64", ""}, "kawarimi-darwin-amd64"},
		{buildTarget{"darwin", "arm64", ""}, "kawarimi-darwin-arm64"},
		{buildTarget{"windows", "amd64", ".exe"}, "kawarimi-windows-amd64.exe"},
	}
	for _, c := range cases {
		if got := c.t.binaryName(); got != c.want {
			t.Errorf("binaryName(%v) = %q, want %q", c.t, got, c.want)
		}
	}
	if n := len(crossCompileTargets()); n != 5 {
		t.Errorf("expected 5 cross-compile targets, got %d", n)
	}
}

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

// TestCrossCompileBuildsAllTargets actually builds every target from the module
// source. It is slow (five real `go build` invocations) so it is skipped in -short.
func TestCrossCompileBuildsAllTargets(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real cross-compile in -short mode")
	}
	outDir := t.TempDir()
	built, err := crossCompile("..", outDir) // ".." is the module root from ./cmd
	if err != nil {
		t.Fatalf("crossCompile: %v", err)
	}
	if len(built) != 5 {
		t.Fatalf("expected 5 binaries, got %d: %v", len(built), built)
	}
	for _, name := range built {
		info, err := os.Stat(filepath.Join(outDir, name))
		if err != nil {
			t.Errorf("binary %s not created: %v", name, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("binary %s is empty", name)
		}
	}
}
