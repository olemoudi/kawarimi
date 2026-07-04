package deadswitch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/olemoudi/kawarimi/internal/atomicfile"
	gosync "github.com/olemoudi/kawarimi/internal/sync"
	"github.com/olemoudi/kawarimi/internal/vault"
)

// ErrLocalCheckin marks a failure to write the local heartbeat. It is always fatal:
// a check-in that did not even land locally is a phantom check-in and must never be
// reported as success.
var ErrLocalCheckin = errors.New("writing local check-in")

// dmsHeartbeatFile is the check-in file name at the root of the DMS repo. It must
// match the path the generated workflow reads (see GenerateGitHubDMSWorkflow).
const dmsHeartbeatFile = "last_checkin"

// CheckinTargets describes where a check-in must be recorded.
type CheckinTargets struct {
	VaultDir   string // canonical local last_checkin (read by Evaluate/status/TUI)
	DMSRepoDir string // local clone of the DMS heartbeat repo; "" disables the cloud push
	DMSRemote  string // SSH URL of the DMS repo; "" disables the cloud push
}

// RecordCheckin writes the local check-in timestamp and, when a DMS repo is
// configured, pushes it to the standalone heartbeat repo that the GitHub Actions
// dead man's switch reads. The local write always happens; a cloud push failure is
// returned so the caller can warn loudly — if the cloud never sees the check-in the
// switch will keep escalating and eventually release while the owner is alive.
//
// pushed reports whether the cloud heartbeat was updated.
func RecordCheckin(t CheckinTargets, now time.Time) (pushed bool, err error) {
	stamp := now.UTC().Format(time.RFC3339)

	localPath := filepath.Join(t.VaultDir, vault.LastCheckinFile)
	if err := atomicfile.WriteFile(localPath, []byte(stamp+"\n"), 0600); err != nil {
		return false, fmt.Errorf("%w: %v", ErrLocalCheckin, err)
	}

	if t.DMSRepoDir == "" || t.DMSRemote == "" {
		return false, nil // no cloud DMS configured
	}

	if err := pushDMSHeartbeat(t.DMSRepoDir, t.DMSRemote, stamp); err != nil {
		return false, err
	}
	return true, nil
}

func pushDMSHeartbeat(repoDir, remote, stamp string) error {
	if err := os.MkdirAll(repoDir, 0700); err != nil {
		return fmt.Errorf("creating DMS repo dir: %w", err)
	}

	gs := gosync.NewGitSync(repoDir, remote, "")
	// Bring the local clone in line with the remote first so a second device
	// checking in cannot create a divergent history that go-git cannot push.
	if err := gs.ResetToRemote(); err != nil {
		return fmt.Errorf("syncing DMS repo from remote: %w", err)
	}

	if err := os.WriteFile(filepath.Join(repoDir, dmsHeartbeatFile), []byte(stamp+"\n"), 0644); err != nil {
		return fmt.Errorf("writing DMS heartbeat: %w", err)
	}

	if err := gs.CommitAndPush("checkin " + stamp); err != nil {
		return fmt.Errorf("pushing DMS heartbeat: %w", err)
	}
	return nil
}
