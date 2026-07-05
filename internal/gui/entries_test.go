package gui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/olemoudi/kawarimi/internal/crypto"
	"github.com/olemoudi/kawarimi/internal/setup"
	"github.com/olemoudi/kawarimi/internal/testenv"
)

const testPassword = "gui-test-password-123"

// newUnlockedServer creates a fast-KDF vault in an isolated HOME and returns a
// server with the session already unlocked.
func newUnlockedServer(t *testing.T) *server {
	t.Helper()
	home := testenv.SetHome(t, t.TempDir())
	fast := crypto.TestParams()
	if _, err := setup.InitVault(setup.InitOptions{
		VaultDir:          filepath.Join(home, "vault"),
		Password:          testPassword,
		MnemonicKDFParams: &fast,
		OwnerKDFParams:    &fast,
	}); err != nil {
		t.Fatalf("InitVault: %v", err)
	}
	s := &server{
		token: testToken, addr: "127.0.0.1:9999", port: "9999",
		opts: Options{Version: "test"}, sess: &session{}, lastSeen: time.Now(), quit: make(chan struct{}),
	}
	if err := s.sess.unlock(testPassword); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	return s
}

// call issues a request with the session cookie + a valid Origin and returns the recorder.
func call(h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	var r *http.Request
	if body != nil {
		buf, _ := json.Marshal(body)
		r = httptest.NewRequest(method, "http://127.0.0.1:9999"+path, bytes.NewReader(buf))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, "http://127.0.0.1:9999"+path, nil)
	}
	r.Header.Set("Origin", "http://127.0.0.1:9999")
	r.AddCookie(&http.Cookie{Name: cookieName, Value: testToken})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func TestEntriesCRUD(t *testing.T) {
	h := newUnlockedServer(t).routes()

	// Create a note.
	rec := call(h, "POST", "/api/entries", map[string]any{
		"category": "notes", "title": "Bank", "content": "acct 123",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create note: %d (%s)", rec.Code, rec.Body)
	}
	var created entrySummary
	json.Unmarshal(rec.Body.Bytes(), &created)
	if created.ID == "" || created.Category != "notes" {
		t.Fatalf("unexpected created entry %+v", created)
	}

	// Create a credential.
	if rec := call(h, "POST", "/api/entries", map[string]any{
		"category": "credentials", "credential": map[string]any{"service": "Email", "username": "me", "password": "pw"},
	}); rec.Code != http.StatusOK {
		t.Fatalf("create credential: %d (%s)", rec.Code, rec.Body)
	}

	// List shows both.
	rec = call(h, "GET", "/api/entries", nil)
	var list []entrySummary
	json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(list))
	}

	// Get the note's content.
	rec = call(h, "GET", "/api/entries/"+created.ID, nil)
	var got map[string]any
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got["content"] != "acct 123" {
		t.Errorf("note content = %v, want 'acct 123'", got["content"])
	}

	// Update it.
	if rec := call(h, "PUT", "/api/entries/"+created.ID, map[string]any{
		"category": "notes", "content": "acct 999",
	}); rec.Code != http.StatusOK {
		t.Fatalf("update: %d (%s)", rec.Code, rec.Body)
	}
	rec = call(h, "GET", "/api/entries/"+created.ID, nil)
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got["content"] != "acct 999" {
		t.Errorf("after update, content = %v, want 'acct 999'", got["content"])
	}

	// Delete it.
	if rec := call(h, "DELETE", "/api/entries/"+created.ID, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete: %d", rec.Code)
	}
	rec = call(h, "GET", "/api/entries", nil)
	json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 {
		t.Errorf("after delete, expected 1 entry, got %d", len(list))
	}
}

func TestEntryGetNotFound(t *testing.T) {
	h := newUnlockedServer(t).routes()
	if rec := call(h, "GET", "/api/entries/nonexistent", nil); rec.Code != http.StatusNotFound {
		t.Errorf("missing entry: got %d, want 404", rec.Code)
	}
}

func TestEntriesRequireUnlocked(t *testing.T) {
	// A locked session must not list entries.
	testenv.SetHome(t, t.TempDir())
	s := &server{
		token: testToken, addr: "127.0.0.1:9999", port: "9999",
		opts: Options{}, sess: &session{}, lastSeen: time.Now(), quit: make(chan struct{}),
	}
	if rec := call(s.routes(), "GET", "/api/entries", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("locked list: got %d, want 400", rec.Code)
	}
	_ = os.Getenv("HOME")
}

// The entries API must reject malformed writes with a 400 and a clear message,
// never a 500 or a silent partial write.
func TestEntriesValidationErrors(t *testing.T) {
	h := newUnlockedServer(t).routes()

	cases := []struct {
		name   string
		method string
		path   string
		body   map[string]any
		want   string
	}{
		{"note without title", "POST", "/api/entries",
			map[string]any{"category": "notes", "content": "x"}, "title"},
		{"credential without service", "POST", "/api/entries",
			map[string]any{"category": "credentials", "title": "Bank",
				"credential": map[string]any{"username": "u"}}, "service"},
		{"unsupported category", "POST", "/api/entries",
			map[string]any{"category": "documents", "title": "Doc"}, "CLI"},
	}
	for _, tc := range cases {
		rec := call(h, tc.method, tc.path, tc.body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400 (%s)", tc.name, rec.Code, rec.Body)
			continue
		}
		if !strings.Contains(rec.Body.String(), tc.want) {
			t.Errorf("%s: error %q should mention %q", tc.name, rec.Body.String(), tc.want)
		}
	}

	// Update-specific validation: a credential update needs credential fields,
	// and documents cannot be replaced through the GUI.
	rec := call(h, "POST", "/api/entries", map[string]any{
		"category": "credentials", "title": "Bank",
		"credential": map[string]any{"service": "Bank", "username": "u", "password": "p"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create credential: %d (%s)", rec.Code, rec.Body)
	}
	var created entrySummary
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if rec := call(h, "PUT", "/api/entries/"+created.ID, map[string]any{
		"category": "credentials",
	}); rec.Code != http.StatusBadRequest {
		t.Errorf("credential update without fields: got %d, want 400", rec.Code)
	}
	if rec := call(h, "PUT", "/api/entries/nonexistent", map[string]any{
		"category": "notes", "content": "x",
	}); rec.Code != http.StatusNotFound {
		t.Errorf("update of a missing entry: got %d, want 404", rec.Code)
	}
}
