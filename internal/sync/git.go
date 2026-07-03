package sync

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

// gitOpTimeout bounds network git operations. Check-in evaluation runs unattended
// under a systemd timer and must never hang on a dead network.
const gitOpTimeout = 30 * time.Second

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

	// New repos are initialized on `main`. GitHub Actions scheduled workflows only
	// run on the repository's default branch; go-git otherwise defaults to `master`,
	// which would leave a pushed workflow that never triggers.
	repo, err = git.PlainInitWithOptions(g.VaultDir, &git.PlainInitOptions{
		InitOptions: git.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName("main")},
	})
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
		Name:  "origin",
		URLs:  []string{g.RemoteURL},
		Fetch: []gitconfig.RefSpec{"+refs/heads/*:refs/remotes/origin/*"},
	})
	if err != nil {
		return fmt.Errorf("creating remote: %w", err)
	}

	return nil
}

// Sync performs add, commit, and push with a default vault-update message.
func (g *GitSync) Sync() error {
	return g.CommitAndPush(fmt.Sprintf("kawarimi vault update %s", time.Now().UTC().Format(time.RFC3339)))
}

// Commit stages everything under the repo and commits with the given message
// when there are changes. It does not push. Returns the repository handle so
// callers can push or force-push separately.
func (g *GitSync) Commit(message string) (*git.Repository, error) {
	repo, err := g.EnsureRepo()
	if err != nil {
		return nil, err
	}

	if g.RemoteURL != "" {
		if err := g.EnsureRemote(repo); err != nil {
			return nil, err
		}
	}

	wt, err := repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("getting worktree: %w", err)
	}

	// Add all files
	if err := wt.AddGlob("."); err != nil {
		return nil, fmt.Errorf("staging files: %w", err)
	}

	// Check if there are changes to commit
	status, err := wt.Status()
	if err != nil {
		return nil, fmt.Errorf("checking status: %w", err)
	}

	if status.IsClean() {
		return repo, nil
	}

	_, err = wt.Commit(message, &git.CommitOptions{
		Author: &object.Signature{
			Name:  "kawarimi",
			Email: "kawarimi@vault",
			When:  time.Now(),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("committing: %w", err)
	}

	return repo, nil
}

// CommitAndPush stages everything in the repo, commits (when there are changes)
// with the given message, and pushes to origin.
func (g *GitSync) CommitAndPush(message string) error {
	repo, err := g.Commit(message)
	if err != nil {
		return err
	}
	return g.push(repo)
}

// ResetToRemote fetches origin and hard-resets the worktree to origin/main.
// It is best-effort: if the remote is unreachable, or has no main branch yet
// (freshly created repo), it returns nil so the caller can proceed to write and
// push. Resetting before a check-in keeps a local clone from diverging when the
// owner checks in from more than one device.
func (g *GitSync) ResetToRemote() error {
	repo, err := g.EnsureRepo()
	if err != nil {
		return err
	}
	if g.RemoteURL == "" {
		return nil
	}
	if err := g.EnsureRemote(repo); err != nil {
		return err
	}

	auth, err := g.authFor()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), gitOpTimeout)
	defer cancel()

	err = repo.FetchContext(ctx, &git.FetchOptions{RemoteName: "origin", Auth: auth})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return nil // offline, or nothing on the remote yet — nothing to reset to
	}

	ref, err := repo.Reference(plumbing.NewRemoteReferenceName("origin", "main"), true)
	if err != nil {
		return nil // remote main doesn't exist yet
	}

	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("getting worktree: %w", err)
	}

	// On a fresh clone the local main branch is unborn; go-git's Reset cannot
	// reset from an unborn HEAD, so point main at the remote tip first.
	if _, err := repo.Head(); err != nil {
		mainRef := plumbing.NewHashReference(plumbing.NewBranchReferenceName("main"), ref.Hash())
		if err := repo.Storer.SetReference(mainRef); err != nil {
			return fmt.Errorf("setting main ref: %w", err)
		}
	}

	return wt.Reset(&git.ResetOptions{Commit: ref.Hash(), Mode: git.HardReset})
}

// ForcePush force-pushes the local main branch to origin, overwriting remote
// history. Used only to repair a diverged heartbeat repo (switch seed --force).
func (g *GitSync) ForcePush() error {
	if g.RemoteURL == "" {
		return nil
	}
	repo, err := git.PlainOpen(g.VaultDir)
	if err != nil {
		return fmt.Errorf("opening repo: %w", err)
	}

	auth, err := g.authFor()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), gitOpTimeout)
	defer cancel()

	err = repo.PushContext(ctx, &git.PushOptions{
		RemoteName: "origin",
		Auth:       auth,
		RefSpecs:   []gitconfig.RefSpec{"+refs/heads/main:refs/heads/main"},
		Force:      true,
	})
	if err == git.NoErrAlreadyUpToDate {
		return nil
	}
	if err != nil {
		return fmt.Errorf("force pushing: %w", err)
	}
	return nil
}

// ReadRemoteFile fetches origin and returns the contents of relPath at origin/main
// without touching the worktree. Used to inspect the DMS repo's state (heartbeat and
// workflow) from another machine.
func (g *GitSync) ReadRemoteFile(relPath string) (string, error) {
	repo, err := g.EnsureRepo()
	if err != nil {
		return "", err
	}
	if err := g.EnsureRemote(repo); err != nil {
		return "", err
	}

	auth, err := g.authFor()
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), gitOpTimeout)
	defer cancel()

	err = repo.FetchContext(ctx, &git.FetchOptions{RemoteName: "origin", Auth: auth})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return "", fmt.Errorf("fetching remote: %w", err)
	}

	ref, err := repo.Reference(plumbing.NewRemoteReferenceName("origin", "main"), true)
	if err != nil {
		return "", fmt.Errorf("resolving origin/main: %w", err)
	}
	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		return "", fmt.Errorf("reading commit: %w", err)
	}
	f, err := commit.File(relPath)
	if err != nil {
		return "", fmt.Errorf("%s not found on remote: %w", relPath, err)
	}
	return f.Contents()
}

func (g *GitSync) push(repo *git.Repository) error {
	if g.RemoteURL == "" {
		return nil
	}

	auth, err := g.authFor()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), gitOpTimeout)
	defer cancel()

	err = repo.PushContext(ctx, &git.PushOptions{
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

// authFor returns the auth method for the configured remote. Local remotes
// (file:// or filesystem paths, used for backups and in tests) need no auth;
// everything else is treated as SSH.
func (g *GitSync) authFor() (transport.AuthMethod, error) {
	if isLocalRemote(g.RemoteURL) {
		return nil, nil
	}
	return g.sshAuth()
}

func isLocalRemote(url string) bool {
	return url == "" ||
		strings.HasPrefix(url, "file://") ||
		strings.HasPrefix(url, "/") ||
		strings.HasPrefix(url, "./") ||
		strings.HasPrefix(url, "../")
}

func (g *GitSync) sshAuth() (*ssh.PublicKeys, error) {
	auth, err := ssh.NewPublicKeysFromFile("git", g.SSHKey, "")
	if err != nil {
		return nil, fmt.Errorf("loading SSH key from %s: %w", g.SSHKey, err)
	}
	return auth, nil
}
