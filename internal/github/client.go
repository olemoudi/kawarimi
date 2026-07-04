// Package github is a minimal GitHub REST client used by the setup wizard to
// automate the cloud dead man's switch: create the private DMS repo and set the
// Actions secrets. It is pure net/http plus nacl/box (libsodium sealed boxes) for
// secret encryption, so it adds no CGo and no new module dependency.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultBaseURL = "https://api.github.com"

// Client talks to the GitHub REST API with a personal access token.
type Client struct {
	token   string
	baseURL string
	http    *http.Client
}

// NewClient returns a client authenticated with the given personal access token.
func NewClient(token string) *Client {
	return &Client{
		token:   strings.TrimSpace(token),
		baseURL: defaultBaseURL,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// Repo identifies a GitHub repository.
type Repo struct {
	Owner  string
	Name   string
	SSHURL string
}

// do performs an authenticated request and returns the raw response. Callers are
// responsible for closing the body. body may be nil.
func (c *Client) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshaling request: %w", err)
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.http.Do(req)
}

// apiError reads and summarizes a non-2xx response without leaking secrets (this
// client never sends secret values in a body whose echo we return — secret PUTs
// carry only the sealed ciphertext).
func apiError(resp *http.Response, action string) error {
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	msg := strings.TrimSpace(string(data))
	if msg == "" {
		return fmt.Errorf("%s: HTTP %d", action, resp.StatusCode)
	}
	return fmt.Errorf("%s: HTTP %d: %s", action, resp.StatusCode, msg)
}

// AuthenticatedUser returns the login of the token's owner.
func (c *Client) AuthenticatedUser(ctx context.Context) (string, error) {
	resp, err := c.do(ctx, http.MethodGet, "/user", nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", apiError(resp, "get authenticated user")
	}
	var out struct {
		Login string `json:"login"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decoding user: %w", err)
	}
	if out.Login == "" {
		return "", fmt.Errorf("get authenticated user: empty login in response")
	}
	return out.Login, nil
}

type repoResponse struct {
	Name   string `json:"name"`
	SSHURL string `json:"ssh_url"`
	Owner  struct {
		Login string `json:"login"`
	} `json:"owner"`
}

func (r repoResponse) toRepo() Repo {
	return Repo{Owner: r.Owner.Login, Name: r.Name, SSHURL: r.SSHURL}
}

// CreatePrivateRepo creates a private, empty repository under the authenticated
// user. If the repo already exists (HTTP 422), it returns the existing one so the
// wizard is idempotent.
func (c *Client) CreatePrivateRepo(ctx context.Context, name string) (Repo, error) {
	resp, err := c.do(ctx, http.MethodPost, "/user/repos", map[string]any{
		"name":      name,
		"private":   true,
		"auto_init": false,
	})
	if err != nil {
		return Repo{}, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusCreated:
		var rr repoResponse
		if err := json.NewDecoder(resp.Body).Decode(&rr); err != nil {
			return Repo{}, fmt.Errorf("decoding created repo: %w", err)
		}
		return rr.toRepo(), nil
	case http.StatusUnprocessableEntity:
		// Most likely "name already exists on this account" — fall back to fetching it.
		return c.getRepo(ctx, name)
	default:
		return Repo{}, apiError(resp, "create repo")
	}
}

// getRepo fetches an existing repository owned by the authenticated user.
func (c *Client) getRepo(ctx context.Context, name string) (Repo, error) {
	owner, err := c.AuthenticatedUser(ctx)
	if err != nil {
		return Repo{}, err
	}
	resp, err := c.do(ctx, http.MethodGet, "/repos/"+owner+"/"+name, nil)
	if err != nil {
		return Repo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Repo{}, apiError(resp, "get repo")
	}
	var rr repoResponse
	if err := json.NewDecoder(resp.Body).Decode(&rr); err != nil {
		return Repo{}, fmt.Errorf("decoding repo: %w", err)
	}
	return rr.toRepo(), nil
}
