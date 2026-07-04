package gui

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/olemoudi/kawarimi/internal/config"
	"github.com/olemoudi/kawarimi/internal/vault"
)

// handlePackageBuild assembles the distributable recipient package (zip). Like the
// CLI, cross-compiling recipient binaries requires a kawarimi source checkout;
// callers can instead choose "none" to omit binaries.
func (s *server) handlePackageBuild(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var body struct {
		Mode   string `json:"mode"` // "auto" (cross-compile) | "none"
		Output string `json:"output"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	cfg, err := config.Load()
	if err != nil {
		writeError(w, http.StatusBadRequest, "no vault configured")
		return
	}
	if _, err := os.Stat(cfg.VaultDir); err != nil {
		writeError(w, http.StatusBadRequest, "vault directory not found: "+cfg.VaultDir)
		return
	}

	output := strings.TrimSpace(body.Output)
	if output == "" {
		home, _ := os.UserHomeDir()
		output = filepath.Join(home, "kawarimi-vault.zip")
	}

	binariesDir := ""
	var cleanup func()
	if body.Mode != "none" {
		src := s.resolveSourceDir()
		if src == "" {
			writeError(w, http.StatusBadRequest,
				"no kawarimi source found to build recipient binaries — launch 'kawarimi gui' from a source checkout (or pass --source), or choose \"no binaries\"")
			return
		}
		tmp, err := os.MkdirTemp("", "kawarimi-bin-")
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		cleanup = func() { os.RemoveAll(tmp) }
		defer cleanup()
		if _, err := vault.CrossCompile(src, tmp, s.opts.Version); err != nil {
			writeError(w, http.StatusInternalServerError, "cross-compiling: "+err.Error())
			return
		}
		binariesDir = tmp
	}

	if err := vault.BuildPackage(cfg.VaultDir, output, binariesDir); err != nil {
		writeError(w, http.StatusInternalServerError, "building package: "+err.Error())
		return
	}
	info, err := os.Stat(output)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"path":   output,
		"sizeMB": fmt.Sprintf("%.1f", float64(info.Size())/(1024*1024)),
	})
}

// resolveSourceDir returns a kawarimi module source dir (from --source or the cwd),
// or "" if none is found.
func (s *server) resolveSourceDir() string {
	if s.opts.SourceDir != "" {
		return s.opts.SourceDir
	}
	if cwd, err := os.Getwd(); err == nil && isKawarimiModule(cwd) {
		return cwd
	}
	return ""
}

func isKawarimiModule(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	return err == nil && strings.Contains(string(data), "module github.com/olemoudi/kawarimi")
}
