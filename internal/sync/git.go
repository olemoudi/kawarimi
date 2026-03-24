package sync

import (
	"fmt"
	"os"
	"time"

	"github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

// GitSync handles syncing the vault to a git remote.
type GitSync struct {
	VaultDir  string
	RemoteURL string
	SSHKey    string
}

// NewGitSync creates a GitSync with the given configuration.
func NewGitSync(vaultDir, remoteURL, sshKeyPath string) *GitSync {
	if sshKeyPath == "" {
		home, _ := os.UserHomeDir()
		sshKeyPath = home + "/.ssh/id_ed25519"
	}
	return &GitSync{
		VaultDir:  vaultDir,
		RemoteURL: remoteURL,
		SSHKey:    sshKeyPath,
	}
}

// EnsureRepo ensures the vault directory is a git repository.
// If not, initializes one.
func (g *GitSync) EnsureRepo() (*git.Repository, error) {
	repo, err := git.PlainOpen(g.VaultDir)
	if err == nil {
		return repo, nil
	}

	repo, err = git.PlainInit(g.VaultDir, false)
	if err != nil {
		return nil, fmt.Errorf("initializing git repo: %w", err)
	}

	return repo, nil
}

// EnsureRemote ensures the "origin" remote is configured.
func (g *GitSync) EnsureRemote(repo *git.Repository) error {
	_, err := repo.Remote("origin")
	if err == nil {
		return nil
	}

	_, err = repo.CreateRemote(&gitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{g.RemoteURL},
	})
	if err != nil {
		return fmt.Errorf("creating remote: %w", err)
	}

	return nil
}

// Sync performs add, commit, and push.
func (g *GitSync) Sync() error {
	repo, err := g.EnsureRepo()
	if err != nil {
		return err
	}

	if g.RemoteURL != "" {
		if err := g.EnsureRemote(repo); err != nil {
			return err
		}
	}

	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("getting worktree: %w", err)
	}

	// Add all files
	if err := wt.AddGlob("."); err != nil {
		return fmt.Errorf("staging files: %w", err)
	}

	// Check if there are changes to commit
	status, err := wt.Status()
	if err != nil {
		return fmt.Errorf("checking status: %w", err)
	}

	if status.IsClean() {
		fmt.Println("No changes to commit.")
		return g.push(repo)
	}

	// Commit
	_, err = wt.Commit(fmt.Sprintf("kawarimi vault update %s", time.Now().UTC().Format(time.RFC3339)), &git.CommitOptions{
		Author: &object.Signature{
			Name:  "kawarimi",
			Email: "kawarimi@vault",
			When:  time.Now(),
		},
	})
	if err != nil {
		return fmt.Errorf("committing: %w", err)
	}

	return g.push(repo)
}

func (g *GitSync) push(repo *git.Repository) error {
	if g.RemoteURL == "" {
		return nil
	}

	auth, err := g.sshAuth()
	if err != nil {
		return err
	}

	err = repo.Push(&git.PushOptions{
		RemoteName: "origin",
		Auth:       auth,
	})
	if err == git.NoErrAlreadyUpToDate {
		fmt.Println("Remote already up to date.")
		return nil
	}
	if err != nil {
		return fmt.Errorf("pushing: %w", err)
	}

	fmt.Println("Pushed to remote.")
	return nil
}

func (g *GitSync) sshAuth() (*ssh.PublicKeys, error) {
	auth, err := ssh.NewPublicKeysFromFile("git", g.SSHKey, "")
	if err != nil {
		return nil, fmt.Errorf("loading SSH key from %s: %w", g.SSHKey, err)
	}
	return auth, nil
}
