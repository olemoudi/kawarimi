package sync_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5"
	gosync "github.com/olemoudi/kawarimi/internal/sync"
)

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
