package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/olemoudi/kawarimi/internal/config"
	"github.com/olemoudi/kawarimi/internal/deadswitch"
	gosync "github.com/olemoudi/kawarimi/internal/sync"
)

// StoreSwitchPayloadForMode records the switch payload for a V4 vault according to
// the chosen final-release mode: cloud-only (this machine holds no DMS key) or
// local release (this machine can also deliver the key). It reads the DMS key that
// init/rekey stored at ~/.kawarimi/dms-key when localRelease is true.
func StoreSwitchPayloadForMode(appDir string, localRelease bool) error {
	if !localRelease {
		return deadswitch.StoreSwitchCloudOnly(appDir)
	}
	dmsKeyPath := filepath.Join(appDir, "dms-key")
	dmsKeyBase64, err := os.ReadFile(dmsKeyPath)
	if err != nil {
		return fmt.Errorf("reading DMS key: %w", err)
	}
	return deadswitch.StoreSwitchDMSKey(appDir, strings.TrimSpace(string(dmsKeyBase64)))
}

// SeedResult reports what SeedSwitch armed, so callers can print the follow-up
// checklist (GitHub secrets to set) without re-reading state.
type SeedResult struct {
	DMSRemote  string
	LocalClone string
	// DMSKeyValue is the value to set as the DMS_KEY GitHub secret, read from the
	// local ~/.kawarimi/dms-key. Empty if the local copy was already removed.
	DMSKeyValue string
}

// SeedSwitch writes the dead man's switch workflow + a fresh heartbeat + README
// into the local DMS repo clone and pushes them to the DMS remote. It is
// idempotent (safe to run repeatedly to arm or repair the switch).
//
// If cfg has no DMSRemote yet, dmsRemote is used and saved to the config; callers
// that need to prompt for it should do so and pass the result. It does not print.
func SeedSwitch(cfg *config.Config, switchCfg *deadswitch.SwitchConfig, dmsRemote string, force bool) (*SeedResult, error) {
	if cfg.SyncTargets.DMSRemote == "" {
		if strings.TrimSpace(dmsRemote) == "" {
			return nil, fmt.Errorf("a DMS repo SSH URL is required to arm the cloud switch")
		}
		cfg.SyncTargets.DMSRemote = strings.TrimSpace(dmsRemote)
		if err := config.Save(cfg); err != nil {
			return nil, fmt.Errorf("saving config: %w", err)
		}
	}

	dmsRepoDir, err := config.DMSRepoDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dmsRepoDir, 0700); err != nil {
		return nil, fmt.Errorf("creating DMS repo dir: %w", err)
	}

	gs := gosync.NewGitSync(dmsRepoDir, cfg.SyncTargets.DMSRemote, "")
	// Build on top of whatever is already on the remote (best effort).
	if err := gs.ResetToRemote(); err != nil {
		return nil, fmt.Errorf("syncing DMS repo from remote: %w", err)
	}

	if err := deadswitch.GenerateGitHubDMSWorkflowFile(dmsRepoDir, switchCfg); err != nil {
		return nil, err
	}
	stamp := time.Now().UTC().Format(time.RFC3339)
	if err := os.WriteFile(filepath.Join(dmsRepoDir, "last_checkin"), []byte(stamp+"\n"), 0644); err != nil {
		return nil, fmt.Errorf("writing heartbeat: %w", err)
	}
	readme := "# Kawarimi dead man's switch\n\nHeartbeat repo — do not delete.\n`last_checkin` is updated automatically by `kawarimi checkin`.\n"
	if err := os.WriteFile(filepath.Join(dmsRepoDir, "README.md"), []byte(readme), 0644); err != nil {
		return nil, fmt.Errorf("writing README: %w", err)
	}

	if force {
		if _, err := gs.Commit("seed dead man's switch " + stamp); err != nil {
			return nil, err
		}
		if err := gs.ForcePush(); err != nil {
			return nil, fmt.Errorf("force pushing DMS repo: %w", err)
		}
	} else {
		if err := gs.CommitAndPush("seed dead man's switch " + stamp); err != nil {
			return nil, fmt.Errorf("pushing DMS repo (if it already has commits, retry with force): %w", err)
		}
	}

	res := &SeedResult{
		DMSRemote:  cfg.SyncTargets.DMSRemote,
		LocalClone: dmsRepoDir,
	}
	if appDir, aerr := config.AppDirPath(); aerr == nil {
		if data, rerr := os.ReadFile(filepath.Join(appDir, "dms-key")); rerr == nil {
			res.DMSKeyValue = strings.TrimSpace(string(data))
		}
	}
	return res, nil
}
