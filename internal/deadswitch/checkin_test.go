package deadswitch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

func initBareRemote(t *testing.T) string {
	t.Helper()
	remote := filepath.Join(t.TempDir(), "remote.git")
	if _, err := git.PlainInitWithOptions(remote, &git.PlainInitOptions{
		InitOptions: git.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName("main")},
		Bare:        true,
	}); err != nil {
		t.Fatalf("init bare remote: %v", err)
	}
	return remote
}

func cloneAndRead(t *testing.T, remote, name string) string {
	t.Helper()
	dir := t.TempDir()
	if _, err := git.PlainClone(dir, false, &git.CloneOptions{URL: remote}); err != nil {
		t.Fatalf("clone: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("reading %s from remote: %v", name, err)
	}
	return strings.TrimSpace(string(data))
}

func TestRecordCheckinLocalOnly(t *testing.T) {
	vaultDir := t.TempDir()

	pushed, err := RecordCheckin(CheckinTargets{VaultDir: vaultDir}, time.Now())
	if err != nil {
		t.Fatalf("RecordCheckin: %v", err)
	}
	if pushed {
		t.Error("pushed should be false with no DMS configured")
	}
	if _, err := ReadLastCheckin(vaultDir); err != nil {
		t.Errorf("local last_checkin not written: %v", err)
	}
}

func TestRecordCheckinPushesToRemote(t *testing.T) {
	remote := initBareRemote(t)
	vaultDir := t.TempDir()
	repoDir := filepath.Join(t.TempDir(), "dms-repo")

	now := time.Now()
	pushed, err := RecordCheckin(CheckinTargets{VaultDir: vaultDir, DMSRepoDir: repoDir, DMSRemote: remote}, now)
	if err != nil {
		t.Fatalf("RecordCheckin: %v", err)
	}
	if !pushed {
		t.Fatal("pushed should be true when a DMS remote is configured")
	}

	want := now.UTC().Format(time.RFC3339)
	if got := cloneAndRead(t, remote, "last_checkin"); got != want {
		t.Errorf("remote last_checkin = %q, want %q", got, want)
	}
}

// TestRecordCheckinTwoDevices verifies that a second device (a fresh clone with
// no shared history) can check in without a divergent-history push failure.
func TestRecordCheckinTwoDevices(t *testing.T) {
	remote := initBareRemote(t)

	// Device A checks in earlier.
	if _, err := RecordCheckin(CheckinTargets{
		VaultDir:   t.TempDir(),
		DMSRepoDir: filepath.Join(t.TempDir(), "dms"),
		DMSRemote:  remote,
	}, time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("device A: %v", err)
	}

	// Device B (separate clone) checks in later.
	later := time.Now()
	if _, err := RecordCheckin(CheckinTargets{
		VaultDir:   t.TempDir(),
		DMSRepoDir: filepath.Join(t.TempDir(), "dms"),
		DMSRemote:  remote,
	}, later); err != nil {
		t.Fatalf("device B: %v", err)
	}

	want := later.UTC().Format(time.RFC3339)
	if got := cloneAndRead(t, remote, "last_checkin"); got != want {
		t.Errorf("remote last_checkin = %q, want device B's %q", got, want)
	}
}

func TestRecordCheckinPushFailureReportsError(t *testing.T) {
	vaultDir := t.TempDir()
	repoDir := filepath.Join(t.TempDir(), "dms")
	unreachable := filepath.Join(t.TempDir(), "does-not-exist.git")

	pushed, err := RecordCheckin(CheckinTargets{VaultDir: vaultDir, DMSRepoDir: repoDir, DMSRemote: unreachable}, time.Now())
	if err == nil {
		t.Fatal("expected an error pushing to an unreachable remote")
	}
	if pushed {
		t.Error("pushed should be false on push failure")
	}
	// The local check-in must still have been written so the owner is not blocked.
	if _, rerr := ReadLastCheckin(vaultDir); rerr != nil {
		t.Errorf("local last_checkin should be written even when the push fails: %v", rerr)
	}
}
