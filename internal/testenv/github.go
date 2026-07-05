package testenv

import (
	"testing"

	"github.com/olemoudi/kawarimi/internal/simenv"
)

// GitHubServer mocks the GitHub REST API used by the setup wizard. Point
// RepoSSHURL at a local BareRepo so SeedSwitch pushes locally.
type GitHubServer = simenv.GitHubServer

// StartGitHub starts the mock and points github.NewClient at it.
func StartGitHub(t testing.TB) *GitHubServer {
	t.Helper()
	g, err := simenv.StartGitHub()
	if err != nil {
		t.Fatalf("start github mock: %v", err)
	}
	t.Setenv("KAWARIMI_GITHUB_API", g.URL())
	t.Cleanup(g.Close)
	return g
}
