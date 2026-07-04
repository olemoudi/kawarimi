package vault

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// buildTarget is one cross-compilation target.
type buildTarget struct {
	goos, goarch, suffix string
}

func (t buildTarget) binaryName() string {
	return fmt.Sprintf("kawarimi-%s-%s%s", t.goos, t.goarch, t.suffix)
}

// CrossCompileTargets is the set of platforms shipped to recipients. It is kept
// in sync with the Makefile's PLATFORMS and .goreleaser.yml.
func CrossCompileTargets() []string {
	var names []string
	for _, t := range crossCompileTargets() {
		names = append(names, t.binaryName())
	}
	return names
}

func crossCompileTargets() []buildTarget {
	return []buildTarget{
		{"linux", "amd64", ""},
		{"linux", "arm64", ""},
		{"darwin", "amd64", ""},
		{"darwin", "arm64", ""},
		{"windows", "amd64", ".exe"},
	}
}

// CrossCompile builds a static (CGO_ENABLED=0) kawarimi binary for every recipient
// target into outDir and returns the names it produced. sourceDir must be a kawarimi
// module checkout; version is stamped into cmd.version via ldflags.
func CrossCompile(sourceDir, outDir, version string) ([]string, error) {
	ldflags := "-s -w -X github.com/olemoudi/kawarimi/cmd.version=" + version
	var built []string
	for _, t := range crossCompileTargets() {
		name := t.binaryName()
		out := filepath.Join(outDir, name)
		cmd := exec.Command("go", "build", "-trimpath", "-ldflags", ldflags, "-o", out, ".")
		cmd.Dir = sourceDir
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+t.goos, "GOARCH="+t.goarch)
		if out, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("building %s: %w\n%s", name, err, out)
		}
		built = append(built, name)
	}
	return built, nil
}
