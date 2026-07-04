package deadswitch

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	gosync "github.com/olemoudi/kawarimi/internal/sync"
)

// seedDMSRemote creates a bare remote and pushes a workflow + heartbeat into it,
// as `switch seed` would. Returns the remote URL.
func seedDMSRemote(t *testing.T, switchCfg *SwitchConfig, checkin time.Time) string {
	t.Helper()
	remote := initBareRemote(t) // defined in checkin_test.go (same package)

	seedDir := filepath.Join(t.TempDir(), "seed")
	if err := os.MkdirAll(seedDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := GenerateGitHubDMSWorkflowFile(seedDir, switchCfg); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seedDir, "last_checkin"), []byte(checkin.UTC().Format(time.RFC3339)+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := gosync.NewGitSync(seedDir, remote, "").CommitAndPush("seed"); err != nil {
		t.Fatalf("seeding remote: %v", err)
	}
	return remote
}

func writeLocalCheckin(t *testing.T, vaultDir string, ts time.Time) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(vaultDir, "last_checkin"), []byte(ts.UTC().Format(time.RFC3339)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyHealthy(t *testing.T) {
	switchCfg := DefaultSwitchConfig()
	now := time.Now()
	remote := seedDMSRemote(t, switchCfg, now)

	vaultDir := t.TempDir()
	writeLocalCheckin(t, vaultDir, now)

	targets := CheckinTargets{VaultDir: vaultDir, DMSRepoDir: filepath.Join(t.TempDir(), "dms"), DMSRemote: remote}
	report, err := Verify(targets, switchCfg, t.TempDir())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !report.OK() {
		t.Errorf("expected a healthy switch to pass; report=%+v", report)
	}
	if !report.WorkflowUpToDate {
		t.Error("workflow should be up to date")
	}
	if report.RemoteStale {
		t.Error("remote heartbeat should not be stale")
	}
}

func TestVerifyStaleWorkflow(t *testing.T) {
	// Seed with different thresholds so the remote workflow no longer matches
	// what the current generator produces.
	seedCfg := DefaultSwitchConfig()
	seedCfg.Warning1Days = 10
	seedCfg.FinalDays = 20
	now := time.Now()
	remote := seedDMSRemote(t, seedCfg, now)

	vaultDir := t.TempDir()
	writeLocalCheckin(t, vaultDir, now)

	currentCfg := DefaultSwitchConfig() // 14/21/30 -> different YAML
	targets := CheckinTargets{VaultDir: vaultDir, DMSRepoDir: filepath.Join(t.TempDir(), "dms"), DMSRemote: remote}
	report, err := Verify(targets, currentCfg, t.TempDir())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if report.WorkflowUpToDate {
		t.Error("workflow should be detected as out of date")
	}
	if report.OK() {
		t.Error("verify should fail when the workflow is out of date")
	}
}

func TestVerifyStaleHeartbeat(t *testing.T) {
	switchCfg := DefaultSwitchConfig()
	remote := seedDMSRemote(t, switchCfg, time.Now().Add(-5*24*time.Hour)) // remote 5 days old

	vaultDir := t.TempDir()
	writeLocalCheckin(t, vaultDir, time.Now()) // local is now

	targets := CheckinTargets{VaultDir: vaultDir, DMSRepoDir: filepath.Join(t.TempDir(), "dms"), DMSRemote: remote}
	report, err := Verify(targets, switchCfg, t.TempDir())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !report.RemoteStale {
		t.Error("remote heartbeat should be detected as stale")
	}
	if report.OK() {
		t.Error("verify should fail when the remote heartbeat is stale")
	}
}

// The generated workflow carries the current version marker, and an older/absent
// marker in a deployed workflow is detected as outdated so the owner is told to
// re-seed (a workflow security fix must actually reach the cloud).
func TestWorkflowVersionDrift(t *testing.T) {
	current := GenerateGitHubDMSWorkflow(DefaultSwitchConfig())
	if got := parseWorkflowVersion(current); got != DMSWorkflowVersion {
		t.Errorf("generated workflow marker = %d, want %d", got, DMSWorkflowVersion)
	}
	// A pre-marker (dawidd6-era) workflow parses as version 0 → outdated.
	if got := parseWorkflowVersion("name: Dead Man's Switch\non: {}\n"); got != 0 {
		t.Errorf("marker-less workflow should parse as 0, got %d", got)
	}
	if !(0 < DMSWorkflowVersion) {
		t.Error("a version-0 deployed workflow must be considered outdated")
	}
}
