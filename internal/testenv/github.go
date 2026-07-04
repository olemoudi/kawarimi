package testenv

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/nacl/box"
)

// GitHubServer mocks the GitHub REST API used by the setup wizard: it creates repos,
// serves an Actions secrets public key, and decrypts the sealed secrets it receives
// so tests can assert on their plaintext. It sets KAWARIMI_GITHUB_API so
// github.NewClient routes here.
//
// RepoSSHURL is returned as the created repo's ssh_url; point it at a local BareRepo
// so that SeedSwitch pushes the heartbeat locally instead of over the network.
type GitHubServer struct {
	RepoSSHURL string

	srv  *httptest.Server
	pub  *[32]byte
	priv *[32]byte

	mu      sync.Mutex
	repos   []string
	secrets map[string]string
}

// StartGitHub starts the mock and points github.NewClient at it.
func StartGitHub(t testing.TB) *GitHubServer {
	t.Helper()
	pub, priv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("github mock keypair: %v", err)
	}
	g := &GitHubServer{pub: pub, priv: priv, secrets: map[string]string{}}
	g.srv = httptest.NewServer(http.HandlerFunc(g.handle))
	t.Setenv("KAWARIMI_GITHUB_API", g.srv.URL)
	t.Cleanup(g.srv.Close)
	return g
}

// Secret returns a captured (decrypted) Actions secret and whether it was set.
func (g *GitHubServer) Secret(name string) (string, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	v, ok := g.secrets[name]
	return v, ok
}

// Secrets returns a copy of all captured secrets.
func (g *GitHubServer) Secrets() map[string]string {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make(map[string]string, len(g.secrets))
	for k, v := range g.secrets {
		out[k] = v
	}
	return out
}

// ReposCreated returns the names of repos created via the API.
func (g *GitHubServer) ReposCreated() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.repos...)
}

func (g *GitHubServer) handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	p := r.URL.Path
	switch {
	case r.Method == http.MethodGet && p == "/user":
		json.NewEncoder(w).Encode(map[string]any{"login": "testowner"})

	case r.Method == http.MethodPost && p == "/user/repos":
		var body struct {
			Name string `json:"name"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		g.mu.Lock()
		g.repos = append(g.repos, body.Name)
		g.mu.Unlock()
		ssh := g.RepoSSHURL
		if ssh == "" {
			ssh = "git@github.com:testowner/" + body.Name + ".git"
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"name": body.Name, "ssh_url": ssh, "owner": map[string]any{"login": "testowner"},
		})

	case r.Method == http.MethodGet && strings.HasSuffix(p, "/actions/secrets/public-key"):
		json.NewEncoder(w).Encode(map[string]any{
			"key_id": "test-key-1",
			"key":    base64.StdEncoding.EncodeToString(g.pub[:]),
		})

	case r.Method == http.MethodPut && strings.Contains(p, "/actions/secrets/"):
		var body struct {
			EncryptedValue string `json:"encrypted_value"`
			KeyID          string `json:"key_id"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		name := path.Base(p)
		if plain, ok := g.open(body.EncryptedValue); ok {
			g.mu.Lock()
			g.secrets[name] = plain
			g.mu.Unlock()
		}
		w.WriteHeader(http.StatusCreated)

	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// open decrypts a base64 sealed box with the mock's private key.
func (g *GitHubServer) open(encB64 string) (string, bool) {
	ct, err := base64.StdEncoding.DecodeString(encB64)
	if err != nil {
		return "", false
	}
	plain, ok := box.OpenAnonymous(nil, ct, g.pub, g.priv)
	if !ok {
		return "", false
	}
	return string(plain), true
}
