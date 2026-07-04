package vault

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
	if n := len(CrossCompileTargets()); n != 5 {
		t.Errorf("expected 5 exported target names, got %d", n)
	}
}

// TestCrossCompileBuildsAllTargets actually builds every target from the module
// source. It is slow (five real `go build` invocations) so it is skipped in -short.
func TestCrossCompileBuildsAllTargets(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real cross-compile in -short mode")
	}
	outDir := t.TempDir()
	built, err := CrossCompile("../..", outDir, "test") // "../.." is the module root from ./internal/vault
	if err != nil {
		t.Fatalf("CrossCompile: %v", err)
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
