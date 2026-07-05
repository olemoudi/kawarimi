package simenv

import (
	"fmt"
	"os"
	"path/filepath"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// InitBareRepo initializes dir as an empty bare git repo (default branch main). It
// stands in for the cloud DMS heartbeat repo; local remotes need no SSH.
func InitBareRepo(dir string) error {
	if _, err := git.PlainInitWithOptions(dir, &git.PlainInitOptions{
		Bare:        true,
		InitOptions: git.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName("main")},
	}); err != nil {
		return fmt.Errorf("init bare repo: %w", err)
	}
	return nil
}

// RepoFile clones the bare repo into a throwaway dir and returns the contents of
// name and whether it exists. An empty remote (nothing pushed yet) reports the file
// as missing, not as an error.
func RepoFile(bareRepo, name string) (string, bool, error) {
	work, err := os.MkdirTemp("", "kawarimi-clone-")
	if err != nil {
		return "", false, err
	}
	defer os.RemoveAll(work)
	if _, err := git.PlainClone(work, false, &git.CloneOptions{URL: bareRepo}); err != nil {
		// An empty remote (nothing pushed yet) has no files.
		return "", false, nil
	}
	data, err := os.ReadFile(filepath.Join(work, name))
	if err != nil {
		return "", false, nil
	}
	return string(data), true, nil
}
