package github

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/crypto/nacl/box"
)

// newTestClient points a client at a test server.
func newTestClient(url string) *Client {
	c := NewClient("test-token")
	c.baseURL = url
	return c
}

func TestAuthenticatedUser(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q", got)
		}
		json.NewEncoder(w).Encode(map[string]any{"login": "olemoudi"})
	}))
	defer srv.Close()

	login, err := newTestClient(srv.URL).AuthenticatedUser(context.Background())
	if err != nil {
		t.Fatalf("AuthenticatedUser: %v", err)
	}
	if login != "olemoudi" {
		t.Errorf("login = %q, want olemoudi", login)
	}
}

func TestCreatePrivateRepo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/user/repos" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["private"] != true {
			t.Errorf("expected private:true, got %v", body["private"])
		}
		if body["auto_init"] != false {
			t.Errorf("expected auto_init:false, got %v", body["auto_init"])
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"name":    body["name"],
			"ssh_url": "git@github.com:olemoudi/dms.git",
			"owner":   map[string]any{"login": "olemoudi"},
		})
	}))
	defer srv.Close()

	repo, err := newTestClient(srv.URL).CreatePrivateRepo(context.Background(), "dms")
	if err != nil {
		t.Fatalf("CreatePrivateRepo: %v", err)
	}
	if repo.Owner != "olemoudi" || repo.Name != "dms" || repo.SSHURL != "git@github.com:olemoudi/dms.git" {
		t.Errorf("unexpected repo %+v", repo)
	}
}

// TestCreatePrivateRepoExists exercises the 422 → fetch-existing fallback.
func TestCreatePrivateRepoExists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/user/repos":
			w.WriteHeader(http.StatusUnprocessableEntity)
			io.WriteString(w, `{"message":"name already exists on this account"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/user":
			json.NewEncoder(w).Encode(map[string]any{"login": "olemoudi"})
		case r.Method == http.MethodGet && r.URL.Path == "/repos/olemoudi/dms":
			json.NewEncoder(w).Encode(map[string]any{
				"name":    "dms",
				"ssh_url": "git@github.com:olemoudi/dms.git",
				"owner":   map[string]any{"login": "olemoudi"},
			})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	repo, err := newTestClient(srv.URL).CreatePrivateRepo(context.Background(), "dms")
	if err != nil {
		t.Fatalf("CreatePrivateRepo(exists): %v", err)
	}
	if repo.Name != "dms" || repo.SSHURL == "" {
		t.Errorf("unexpected repo %+v", repo)
	}
}

// TestSetActionsSecretsRoundTrip proves the sealed value the client sends can be
// opened with the server's private key — i.e. the sealing is correct.
func TestSetActionsSecretsRoundTrip(t *testing.T) {
	pub, priv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubB64 := base64.StdEncoding.EncodeToString(pub[:])

	got := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/olemoudi/dms/actions/secrets/public-key":
			json.NewEncoder(w).Encode(publicKey{KeyID: "key-1", Key: pubB64})
		case r.Method == http.MethodPut:
			var body struct {
				EncryptedValue string `json:"encrypted_value"`
				KeyID          string `json:"key_id"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if body.KeyID != "key-1" {
				t.Errorf("key_id = %q, want key-1", body.KeyID)
			}
			ciphertext, err := base64.StdEncoding.DecodeString(body.EncryptedValue)
			if err != nil {
				t.Fatalf("decoding encrypted_value: %v", err)
			}
			plain, ok := box.OpenAnonymous(nil, ciphertext, pub, priv)
			if !ok {
				t.Fatal("could not open sealed box — sealing is wrong")
			}
			// Path: /repos/olemoudi/dms/actions/secrets/<NAME>
			name := r.URL.Path[len("/repos/olemoudi/dms/actions/secrets/"):]
			got[name] = string(plain)
			w.WriteHeader(http.StatusCreated)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	want := map[string]string{"DMS_KEY": "super-secret", "SMTP_PASSWORD": "hunter2"}
	if err := newTestClient(srv.URL).SetActionsSecrets(context.Background(), "olemoudi", "dms", want); err != nil {
		t.Fatalf("SetActionsSecrets: %v", err)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("secret %s round-tripped as %q, want %q", k, got[k], v)
		}
	}
}
