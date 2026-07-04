package gui

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/olemoudi/kawarimi/internal/config"
	"github.com/olemoudi/kawarimi/internal/deadswitch"
)

// writeJSON encodes v as a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError sends {"error": msg} with the given status code.
func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// decodeJSON reads a JSON request body into dst, capping the size.
func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	return dec.Decode(dst)
}

// requireMethod enforces the HTTP method, writing 405 otherwise.
func requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method != method {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return false
	}
	return true
}

type stateResponse struct {
	Configured       bool   `json:"configured"`
	Unlocked         bool   `json:"unlocked"`
	HasDeviceKey     bool   `json:"hasDeviceKey"`
	SwitchConfigured bool   `json:"switchConfigured"`
	CloudOnly        bool   `json:"cloudOnly"`
	VaultDir         string `json:"vaultDir"`
	CheckinInterval  int    `json:"checkinInterval"`
	LastCheckin      string `json:"lastCheckin"`
	DaysSince        int    `json:"daysSince"`
	EntryCount       int    `json:"entryCount"`
	Version          string `json:"version"`
	// Escalation thresholds (0 when the switch isn't configured); the dashboard
	// timeline renders the real warning/release schedule from these.
	Warning1Days int `json:"warning1Days"`
	Warning2Days int `json:"warning2Days"`
	FinalDays    int `json:"finalDays"`
}

// buildState gathers the current state for the SPA (wizard vs unlock vs dashboard).
func (s *server) buildState() stateResponse {
	resp := stateResponse{Version: s.opts.Version, DaysSince: -1}

	appDir, _ := config.AppDirPath()
	if appDir != "" {
		if _, err := os.Stat(filepath.Join(appDir, "device.key")); err == nil {
			resp.HasDeviceKey = true
		}
		resp.SwitchConfigured = deadswitch.IsSwitchConfigured(appDir)
		resp.CloudOnly = deadswitch.SwitchIsCloudOnly(appDir)
		if resp.SwitchConfigured {
			if sc, err := deadswitch.LoadSwitchConfig(appDir); err == nil {
				resp.Warning1Days = sc.Warning1Days
				resp.Warning2Days = sc.Warning2Days
				resp.FinalDays = sc.FinalDays
			}
		}
	}

	cfg, err := config.Load()
	if err == nil {
		resp.Configured = true
		resp.VaultDir = cfg.VaultDir
		resp.CheckinInterval = cfg.CheckinInterval
		if t, err := deadswitch.ReadLastCheckin(cfg.VaultDir); err == nil {
			resp.LastCheckin = t.Format("2006-01-02 15:04 MST")
			if d, err := deadswitch.DaysSinceCheckin(cfg.VaultDir); err == nil {
				resp.DaysSince = d
			}
		}
	}

	resp.Unlocked = s.sess.isUnlocked()
	resp.EntryCount = s.sess.vaultEntryCount()
	return resp
}

// handleState reports enough for the SPA to route (wizard vs unlock vs dashboard).
func (s *server) handleState(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	writeJSON(w, http.StatusOK, s.buildState())
}

// handlePing is a lightweight keepalive; the touch already happened in middleware.
func (s *server) handlePing(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleUnlock opens the vault with the owner password + this device's key.
func (s *server) handleUnlock(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := s.sess.unlock(body.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.buildState())
}

// handleQuit shuts the server down.
func (s *server) handleQuit(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	s.requestQuit()
}
