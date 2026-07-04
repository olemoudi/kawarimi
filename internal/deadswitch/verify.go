package deadswitch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	gosync "github.com/olemoudi/kawarimi/internal/sync"
)

// githubScheduledDisableDays is GitHub's auto-disable window for scheduled
// workflows in a repo with no other activity. FinalDays must stay comfortably
// below it or the switch could be disabled before it ever fires.
const githubScheduledDisableDays = 55

// VerifyReport summarizes the health of the dead man's switch.
type VerifyReport struct {
	DMSConfigured bool
	DMSRemote     string

	LocalCheckin    time.Time
	LocalCheckinErr error

	RemoteCheckin    time.Time
	RemoteCheckinErr error
	RemoteStale      bool // remote heartbeat lags the local one (pushes not landing)

	WorkflowPresent  bool
	WorkflowUpToDate bool
	// DeployedWorkflowVersion is the automation generation running in the DMS repo
	// (0 = pre-marker, e.g. the original dawidd6 workflow). WorkflowOutdated is true
	// when it lags this binary's DMSWorkflowVersion — the owner should re-run
	// `switch seed` so a workflow improvement/security fix actually reaches the cloud.
	DeployedWorkflowVersion int
	WorkflowOutdated        bool

	Triggered           bool
	SystemdTimerActive  bool
	FinalDaysRisky      bool // FinalDays >= githubScheduledDisableDays
	LegacyMnemonicEmail bool // stored payload emails the mnemonic outright (insecure)
}

// remoteStaleThreshold is how far the remote heartbeat may lag the local one
// before we treat it as a plumbing failure rather than clock skew.
const remoteStaleThreshold = 48 * time.Hour

// Verify inspects local and cloud switch state and returns a report. It performs
// network I/O (fetching the DMS repo) when a DMS remote is configured.
func Verify(targets CheckinTargets, switchCfg *SwitchConfig, appDir string) (*VerifyReport, error) {
	r := &VerifyReport{
		DMSConfigured:  targets.DMSRemote != "",
		DMSRemote:      targets.DMSRemote,
		FinalDaysRisky: switchCfg.FinalDays >= githubScheduledDisableDays,
	}

	if lc, err := ReadLastCheckin(targets.VaultDir); err != nil {
		r.LocalCheckinErr = err
	} else {
		r.LocalCheckin = lc
	}

	if _, err := os.Stat(filepath.Join(appDir, "switch-triggered")); err == nil {
		r.Triggered = true
	}

	r.SystemdTimerActive = systemdTimerActive()

	// A legacy V2 payload with email delivery mails the 8 mnemonic words outright.
	if payload, perr := DecryptSwitchPayload(appDir); perr == nil {
		if strings.HasPrefix(payload, "MNEMONIC:") && switchCfg.MnemonicDelivery == "email" {
			r.LegacyMnemonicEmail = true
		}
	}

	if r.DMSConfigured {
		gs := gosync.NewGitSync(targets.DMSRepoDir, targets.DMSRemote, "")

		if content, err := gs.ReadRemoteFile("last_checkin"); err != nil {
			r.RemoteCheckinErr = err
		} else if ts, perr := time.Parse(time.RFC3339, strings.TrimSpace(content)); perr != nil {
			r.RemoteCheckinErr = fmt.Errorf("parsing remote check-in: %w", perr)
		} else {
			r.RemoteCheckin = ts
			if !r.LocalCheckin.IsZero() && r.LocalCheckin.Sub(ts) > remoteStaleThreshold {
				r.RemoteStale = true
			}
		}

		if content, err := gs.ReadRemoteFile(".github/workflows/deadman.yml"); err == nil {
			r.WorkflowPresent = true
			r.WorkflowUpToDate = strings.TrimSpace(content) == strings.TrimSpace(GenerateGitHubDMSWorkflow(switchCfg))
			r.DeployedWorkflowVersion = parseWorkflowVersion(content)
			r.WorkflowOutdated = r.DeployedWorkflowVersion < DMSWorkflowVersion
		}
	}

	return r, nil
}

// OK reports whether the switch is armed and healthy: a local check-in exists and,
// when the cloud DMS is configured, its heartbeat is current and its workflow matches
// what this version of kawarimi generates.
func (r *VerifyReport) OK() bool {
	if r.LocalCheckinErr != nil {
		return false
	}
	if r.DMSConfigured {
		if r.RemoteCheckinErr != nil || r.RemoteStale {
			return false
		}
		if !r.WorkflowPresent || !r.WorkflowUpToDate {
			return false
		}
	}
	return true
}

// parseWorkflowVersion reads the "# kawarimi-dms-workflow-version: N" marker from a
// deployed workflow, or 0 if absent (a pre-marker generation).
func parseWorkflowVersion(content string) int {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "# kawarimi-dms-workflow-version:"); ok {
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				return n
			}
		}
	}
	return 0
}

func systemdTimerActive() bool {
	out, err := exec.Command("systemctl", "--user", "is-active", "kawarimi-switch.timer").Output()
	return err == nil && strings.TrimSpace(string(out)) == "active"
}
