package testenv

import (
	"testing"

	"github.com/olemoudi/kawarimi/internal/simenv"
)

// BareRepo creates an empty bare git repo (default branch main) and returns its
// path. It stands in for the cloud DMS heartbeat repo; local remotes need no SSH.
func BareRepo(t testing.TB) string {
	t.Helper()
	dir := t.TempDir()
	if err := simenv.InitBareRepo(dir); err != nil {
		t.Fatalf("init bare repo: %v", err)
	}
	return dir
}

// RepoFile clones the bare repo and returns the contents of name and whether it
// exists. Use it to assert the heartbeat/workflow actually landed on the remote.
func RepoFile(t testing.TB, bareRepo, name string) (string, bool) {
	t.Helper()
	content, ok, err := simenv.RepoFile(bareRepo, name)
	if err != nil {
		t.Fatalf("reading %s from bare repo: %v", name, err)
	}
	return content, ok
}

// RepoHasFile reports whether name exists on the bare repo's main branch.
func RepoHasFile(t testing.TB, bareRepo, name string) bool {
	_, ok := RepoFile(t, bareRepo, name)
	return ok
}
