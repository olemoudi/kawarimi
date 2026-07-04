package testenv

import (
	"os"
	"path/filepath"
	"testing"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// BareRepo creates an empty bare git repo (default branch main) and returns its
// path. It stands in for the cloud DMS heartbeat repo; local remotes need no SSH.
func BareRepo(t testing.TB) string {
	t.Helper()
	dir := t.TempDir()
	if _, err := git.PlainInitWithOptions(dir, &git.PlainInitOptions{
		Bare:        true,
		InitOptions: git.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName("main")},
	}); err != nil {
		t.Fatalf("init bare repo: %v", err)
	}
	return dir
}

// RepoFile clones the bare repo and returns the contents of name and whether it
// exists. Use it to assert the heartbeat/workflow actually landed on the remote.
func RepoFile(t testing.TB, bareRepo, name string) (string, bool) {
	t.Helper()
	work := t.TempDir()
	if _, err := git.PlainClone(work, false, &git.CloneOptions{URL: bareRepo}); err != nil {
		// An empty remote (nothing pushed yet) has no files.
		return "", false
	}
	data, err := os.ReadFile(filepath.Join(work, name))
	if err != nil {
		return "", false
	}
	return string(data), true
}

// RepoHasFile reports whether name exists on the bare repo's main branch.
func RepoHasFile(t testing.TB, bareRepo, name string) bool {
	_, ok := RepoFile(t, bareRepo, name)
	return ok
}
