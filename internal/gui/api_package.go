package gui

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/olemoudi/kawarimi/internal/config"
	"github.com/olemoudi/kawarimi/internal/selfupdate"
	"github.com/olemoudi/kawarimi/internal/vault"
)

// handlePackageBuild assembles the distributable recipient package (zip). Like the
// CLI, it prefers the official verified release binaries and falls back to a local
// cross-compile (which needs a source checkout); "none" omits binaries entirely.
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
	binariesSource := "none" // "official <tag>" | "local" | "none"
	if body.Mode != "none" {
		tmp, err := os.MkdirTemp("", "kawarimi-bin-")
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer os.RemoveAll(tmp)

		// Prefer the official published binaries (Ed25519-verified, and carrying
		// any OS code signatures a release has); fall back to a local build.
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
		defer cancel()
		if tag, err := selfupdate.FetchOfficialBinaries(ctx, tmp); err == nil {
			binariesDir, binariesSource = tmp, "official "+tag
		} else {
			src := s.resolveSourceDir()
			if src == "" {
				writeError(w, http.StatusBadRequest,
					"could not fetch the official release binaries ("+err.Error()+") and no kawarimi source checkout is available for a local build — retry online, launch 'kawarimi gui' from a source checkout (or pass --source), or choose \"no binaries\"")
				return
			}
			if _, cerr := vault.CrossCompile(src, tmp, s.opts.Version); cerr != nil {
				writeError(w, http.StatusInternalServerError, "cross-compiling: "+cerr.Error())
				return
			}
			binariesDir, binariesSource = tmp, "local"
		}
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
		"ok":             true,
		"path":           output,
		"sizeMB":         fmt.Sprintf("%.1f", float64(info.Size())/(1024*1024)),
		"binariesSource": binariesSource,
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
