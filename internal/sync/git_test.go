package sync_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	gosync "github.com/olemoudi/kawarimi/internal/sync"
)

// initBareRemote creates a bare git repo (default branch main, mirroring how
// GitHub creates repos) to act as a local push/fetch target.
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

func TestGitEnsureRepo(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0644)

	gs := gosync.NewGitSync(dir, "", "")

	repo, err := gs.EnsureRepo()
	if err != nil {
		t.Fatalf("EnsureRepo: %v", err)
	}
	if repo == nil {
		t.Fatal("repo should not be nil")
	}

	// Second call should open existing repo
	repo2, err := gs.EnsureRepo()
	if err != nil {
		t.Fatalf("EnsureRepo second call: %v", err)
	}
	if repo2 == nil {
		t.Fatal("repo2 should not be nil")
	}
}

func TestGitSyncLocalOnly(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello"), 0644)
	os.WriteFile(filepath.Join(dir, "manifest.age"), []byte("encrypted"), 0600)

	// No remote — should just init, add, commit
	gs := gosync.NewGitSync(dir, "", "")
	if err := gs.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Verify repo has a commit
	repo, _ := git.PlainOpen(dir)
	ref, err := repo.Head()
	if err != nil {
		t.Fatalf("getting HEAD: %v", err)
	}

	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		t.Fatalf("getting commit: %v", err)
	}
	if commit == nil {
		t.Fatal("commit should exist")
	}
}

func TestGitSyncNoChanges(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello"), 0644)

	gs := gosync.NewGitSync(dir, "", "")

	// First sync
	if err := gs.Sync(); err != nil {
		t.Fatalf("first Sync: %v", err)
	}

	// Second sync (no changes)
	if err := gs.Sync(); err != nil {
		t.Fatalf("second Sync (no changes): %v", err)
	}
}

func TestGitEnsureRepoInitsMainBranch(t *testing.T) {
	dir := t.TempDir()
	gs := gosync.NewGitSync(dir, "", "")

	repo, err := gs.EnsureRepo()
	if err != nil {
		t.Fatalf("EnsureRepo: %v", err)
	}

	// Commit so HEAD resolves to a concrete branch.
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0644)
	if _, err := gs.Commit("init"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	head, err := repo.Head()
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head.Name().String() != "refs/heads/main" {
		t.Errorf("default branch = %s, want refs/heads/main", head.Name())
	}
}

// TestResetToRemoteTwoClones exercises the two-device flow: a fresh clone can
// reset to the remote (getting its files), and a stale clone can reset then push
// without a divergent history — which is what keeps RecordCheckin force-push-free.
func TestResetToRemoteTwoClones(t *testing.T) {
	remote := initBareRemote(t)

	// Clone A seeds the remote.
	dirA := t.TempDir()
	gsA := gosync.NewGitSync(dirA, remote, "")
	os.WriteFile(filepath.Join(dirA, "last_checkin"), []byte("A1\n"), 0644)
	if err := gsA.CommitAndPush("a1"); err != nil {
		t.Fatalf("A CommitAndPush: %v", err)
	}

	// Clone B (fresh dir) resets to remote and must receive A's file.
	dirB := t.TempDir()
	gsB := gosync.NewGitSync(dirB, remote, "")
	if err := gsB.ResetToRemote(); err != nil {
		t.Fatalf("B ResetToRemote: %v", err)
	}
	if got := readFile(t, dirB, "last_checkin"); got != "A1" {
		t.Fatalf("B last_checkin = %q, want A1", got)
	}

	// B checks in and pushes.
	os.WriteFile(filepath.Join(dirB, "last_checkin"), []byte("B1\n"), 0644)
	if err := gsB.CommitAndPush("b1"); err != nil {
		t.Fatalf("B CommitAndPush: %v", err)
	}

	// A is now stale; reset brings it forward with no conflict.
	if err := gsA.ResetToRemote(); err != nil {
		t.Fatalf("A ResetToRemote: %v", err)
	}
	if got := readFile(t, dirA, "last_checkin"); got != "B1" {
		t.Errorf("A after reset = %q, want B1", got)
	}
}

func TestResetToRemoteOfflineIsNoop(t *testing.T) {
	dir := t.TempDir()
	// Point at a remote path that does not exist -> fetch fails -> best-effort no-op.
	gs := gosync.NewGitSync(dir, filepath.Join(t.TempDir(), "missing.git"), "")
	if err := gs.ResetToRemote(); err != nil {
		t.Errorf("ResetToRemote should be a no-op when the remote is unreachable, got: %v", err)
	}
}

func TestForcePush(t *testing.T) {
	remote := initBareRemote(t)

	dir := t.TempDir()
	gs := gosync.NewGitSync(dir, remote, "")
	os.WriteFile(filepath.Join(dir, "last_checkin"), []byte("v1\n"), 0644)
	if _, err := gs.Commit("v1"); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := gs.ForcePush(); err != nil {
		t.Fatalf("ForcePush: %v", err)
	}

	got := cloneAndRead(t, remote, "last_checkin")
	if got != "v1" {
		t.Errorf("remote last_checkin = %q, want v1", got)
	}
}

func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return strings.TrimSpace(string(data))
}

func cloneAndRead(t *testing.T, remote, name string) string {
	t.Helper()
	dir := t.TempDir()
	if _, err := git.PlainClone(dir, false, &git.CloneOptions{URL: remote}); err != nil {
		t.Fatalf("clone: %v", err)
	}
	return readFile(t, dir, name)
}
