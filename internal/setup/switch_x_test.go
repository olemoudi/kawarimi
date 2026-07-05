package setup_test

// External test package: testenv imports setup, so these tests (which want the
// full harness) live in setup_test to avoid an import cycle.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/olemoudi/kawarimi/internal/deadswitch"
	"github.com/olemoudi/kawarimi/internal/setup"
	gosync "github.com/olemoudi/kawarimi/internal/sync"
	"github.com/olemoudi/kawarimi/internal/testenv"
)

func testSwitchConfig() *deadswitch.SwitchConfig {
	sc := deadswitch.DefaultSwitchConfig()
	sc.SMTPServer = "smtp.example.test"
	sc.SMTPPort = 587
	sc.SMTPUsername = "bot@example.test"
	sc.SMTPPassword = "pw"
	sc.SenderEmail = "bot@example.test"
	sc.UserEmail = "owner@example.test"
	sc.Recipients = []string{"heir@example.test"}
	sc.VaultPackageLocation = "https://example.test/vault.zip"
	return sc
}

// cloneRemote checks out the current state of a bare repo into a fresh dir.
func cloneRemote(t *testing.T, remote string) string {
	t.Helper()
	dir := t.TempDir()
	if err := gosync.NewGitSync(dir, remote, "").ResetToRemote(); err != nil {
		t.Fatalf("cloning %s: %v", remote, err)
	}
	return dir
}

// TestSeedThenDisableCloudSwitch: `switch disable` must neutralize the REMOTE —
// remove the workflow and push a far-future heartbeat — because the cloud repo,
// not the local machine, is the real post-mortem trigger.
func TestSeedThenDisableCloudSwitch(t *testing.T) {
	env := testenv.New(t)
	env.InitVault(t)
	remote := testenv.BareRepo(t)
	cfg := env.Config(t)

	res, err := setup.SeedSwitch(cfg, testSwitchConfig(), remote, false)
	if err != nil {
		t.Fatalf("SeedSwitch: %v", err)
	}
	if res.DMSRemote != remote {
		t.Errorf("DMSRemote = %q, want %q", res.DMSRemote, remote)
	}
	if res.DMSKeyValue == "" {
		t.Error("SeedSwitch should surface the DMS key value for the GitHub secret")
	}

	seeded := cloneRemote(t, remote)
	if _, err := os.Stat(filepath.Join(seeded, ".github", "workflows", "deadman.yml")); err != nil {
		t.Fatalf("seeded remote is missing the workflow: %v", err)
	}

	if err := setup.DisableCloudSwitch(cfg); err != nil {
		t.Fatalf("DisableCloudSwitch: %v", err)
	}

	disabled := cloneRemote(t, remote)
	if _, err := os.Stat(filepath.Join(disabled, ".github", "workflows", "deadman.yml")); !os.IsNotExist(err) {
		t.Error("disable must remove the workflow from the remote")
	}
	raw, err := os.ReadFile(filepath.Join(disabled, "last_checkin"))
	if err != nil {
		t.Fatalf("reading disabled heartbeat: %v", err)
	}
	ts, err := time.Parse(time.RFC3339, strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("disabled heartbeat unparseable: %v", err)
	}
	if ts.Before(time.Now().AddDate(50, 0, 0)) {
		t.Errorf("disabled heartbeat %s is not far-future — a lingering workflow could still fire", ts)
	}
}

func TestDisableCloudSwitchWithoutRemoteIsNoOp(t *testing.T) {
	env := testenv.New(t)
	env.InitVault(t)
	cfg := env.Config(t)
	cfg.SyncTargets.DMSRemote = ""
	if err := setup.DisableCloudSwitch(cfg); err != nil {
		t.Fatalf("no-remote disable must be a no-op, got %v", err)
	}
}

// The fail-closed contract: if the remote cannot be reached, disable must report
// an error — callers must not tell the owner the switch is off when it is not.
func TestDisableCloudSwitchUnreachableRemoteFails(t *testing.T) {
	env := testenv.New(t)
	env.InitVault(t)
	cfg := env.Config(t)
	cfg.SyncTargets.DMSRemote = filepath.Join(t.TempDir(), "does-not-exist.git")
	if err := setup.DisableCloudSwitch(cfg); err == nil {
		t.Fatal("unreachable remote must error, not report the switch as disabled")
	}
}
