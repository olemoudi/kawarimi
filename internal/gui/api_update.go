package gui

import (
	"context"
	"net/http"
	"time"

	"github.com/olemoudi/kawarimi/internal/config"
	"github.com/olemoudi/kawarimi/internal/selfupdate"
)

// handleUpdateCheck reports whether a newer signed release is available. It answers
// from the daily cache when fresh, and does a bounded live check otherwise, so the
// dashboard stays fast on repeat loads.
func (s *server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	appDir, err := config.AppDirPath()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	rel, available, fresh := selfupdate.CachedLatest(appDir, s.opts.Version)
	if !fresh {
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		if r2, a2, err := selfupdate.RefreshCache(ctx, appDir, s.opts.Version); err == nil {
			rel, available = r2, a2
		}
	}

	resp := map[string]any{"available": available}
	if available {
		resp["version"] = rel.Version
		resp["url"] = rel.HTMLURL
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleUpdateApply downloads, verifies, and installs the newest release over this
// binary. The running server keeps its in-memory code; the user must restart.
func (s *server) handleUpdateApply(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	rel, available, err := selfupdate.Latest(ctx, s.opts.Version)
	if err != nil {
		writeError(w, http.StatusBadGateway, "checking for updates: "+err.Error())
		return
	}
	if !available {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "upToDate": true})
		return
	}
	if err := selfupdate.Apply(ctx, rel, ""); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": rel.Version, "restart": true})
}
