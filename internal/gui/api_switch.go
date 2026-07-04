package gui

import (
	"errors"
	"net/http"
	"time"

	"github.com/olemoudi/kawarimi/internal/config"
	"github.com/olemoudi/kawarimi/internal/deadswitch"
)

// checkinTargets mirrors the CLI helper: always the local vault, plus the DMS
// heartbeat repo when a DMS remote is configured.
func checkinTargets(cfg *config.Config) (deadswitch.CheckinTargets, error) {
	dmsRepoDir, err := config.DMSRepoDir()
	if err != nil {
		return deadswitch.CheckinTargets{}, err
	}
	return deadswitch.CheckinTargets{
		VaultDir:   cfg.VaultDir,
		DMSRepoDir: dmsRepoDir,
		DMSRemote:  cfg.SyncTargets.DMSRemote,
	}, nil
}

// handleCheckin records a check-in locally and (if configured) pushes the heartbeat.
func (s *server) handleCheckin(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	cfg, err := config.Load()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	targets, err := checkinTargets(cfg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	pushed, cerr := deadswitch.RecordCheckin(targets, time.Now().UTC())
	// A local-write failure is always fatal (a phantom check-in is dangerous); with
	// no cloud DMS the local write is the only failure path.
	if cerr != nil && (cfg.SyncTargets.DMSRemote == "" || errors.Is(cerr, deadswitch.ErrLocalCheckin)) {
		writeError(w, http.StatusInternalServerError, cerr.Error())
		return
	}
	resp := map[string]any{"ok": cerr == nil, "pushed": pushed}
	if cerr != nil {
		resp["cloudError"] = cerr.Error()
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleSwitchVerify runs the end-to-end switch health check.
func (s *server) handleSwitchVerify(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	cfg, err := config.Load()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	appDir, err := config.AppDirPath()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !deadswitch.IsSwitchConfigured(appDir) {
		writeError(w, http.StatusBadRequest, "switch not configured")
		return
	}
	switchCfg, err := deadswitch.LoadSwitchConfig(appDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	targets, err := checkinTargets(cfg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	report, err := deadswitch.Verify(targets, switchCfg, appDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":               report.OK(),
		"dmsConfigured":    report.DMSConfigured,
		"remoteStale":      report.RemoteStale,
		"workflowPresent":  report.WorkflowPresent,
		"workflowUpToDate": report.WorkflowUpToDate,
		"workflowOutdated": report.WorkflowOutdated,
		"triggered":        report.Triggered,
		"finalDaysRisky":   report.FinalDaysRisky,
	})
}
