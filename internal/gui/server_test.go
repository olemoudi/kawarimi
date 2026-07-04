package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/olemoudi/kawarimi/internal/testenv"
)

const testToken = "0123456789abcdef0123456789abcdef"

func testServer(t *testing.T) *server {
	t.Helper()
	testenv.SetHome(t, t.TempDir()) // isolate config lookups
	return &server{
		token:    testToken,
		addr:     "127.0.0.1:9999",
		port:     "9999",
		opts:     Options{Version: "test"},
		sess:     &session{},
		lastSeen: time.Now(),
		quit:     make(chan struct{}),
	}
}

func do(h http.Handler, method, url string, cookie bool, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, url, nil)
	if cookie {
		req.AddCookie(&http.Cookie{Name: cookieName, Value: testToken})
	}
	for k, v := range headers {
		if k == "Host" {
			req.Host = v
		} else {
			req.Header.Set(k, v)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHostAllowlistRejectsForeignHost(t *testing.T) {
	h := testServer(t).routes()
	rec := do(h, "GET", "http://127.0.0.1:9999/api/state", true, map[string]string{"Host": "evil.com"})
	if rec.Code != http.StatusForbidden {
		t.Errorf("foreign Host: got %d, want 403", rec.Code)
	}
}

func TestApiRequiresSessionCookie(t *testing.T) {
	h := testServer(t).routes()
	rec := do(h, "GET", "http://127.0.0.1:9999/api/state", false, nil)
	if rec.Code != http.StatusForbidden {
		t.Errorf("no cookie: got %d, want 403", rec.Code)
	}
}

func TestIndexTokenBootstrapSetsCookie(t *testing.T) {
	h := testServer(t).routes()
	rec := do(h, "GET", "http://127.0.0.1:9999/?t="+testToken, false, nil)
	if rec.Code != http.StatusFound {
		t.Fatalf("token bootstrap: got %d, want 302", rec.Code)
	}
	sc := rec.Result().Cookies()
	if len(sc) != 1 || sc[0].Name != cookieName || sc[0].Value != testToken {
		t.Fatalf("expected session cookie, got %+v", sc)
	}
	if !sc[0].HttpOnly || sc[0].SameSite != http.SameSiteStrictMode {
		t.Errorf("cookie must be HttpOnly + SameSite=Strict, got %+v", sc[0])
	}
}

func TestIndexRejectsWrongToken(t *testing.T) {
	h := testServer(t).routes()
	rec := do(h, "GET", "http://127.0.0.1:9999/?t=wrongtoken", false, nil)
	if rec.Code != http.StatusForbidden {
		t.Errorf("wrong token: got %d, want 403", rec.Code)
	}
}

func TestStateWithCookie(t *testing.T) {
	h := testServer(t).routes()
	rec := do(h, "GET", "http://127.0.0.1:9999/api/state", true, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("state with cookie: got %d, want 200", rec.Code)
	}
	var st stateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("decoding state: %v", err)
	}
	if st.Version != "test" {
		t.Errorf("version = %q, want test", st.Version)
	}
	if st.Configured {
		t.Error("expected not-configured in an isolated HOME")
	}
}

func TestCSPHeaderPresent(t *testing.T) {
	h := testServer(t).routes()
	rec := do(h, "GET", "http://127.0.0.1:9999/api/state", true, nil)
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'none'") || !strings.Contains(csp, "connect-src 'self'") {
		t.Errorf("unexpected CSP: %q", csp)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("missing no-store cache header")
	}
}

func TestMutatingRequestRejectsForeignOrigin(t *testing.T) {
	h := testServer(t).routes()
	rec := do(h, "POST", "http://127.0.0.1:9999/api/unlock", true, map[string]string{"Origin": "http://evil.com"})
	if rec.Code != http.StatusForbidden {
		t.Errorf("foreign Origin on POST: got %d, want 403", rec.Code)
	}
}

// The idle watchdog must never fire while a request (e.g. a multi-minute package
// build) is in flight, and the idle countdown restarts when the work finishes.
func TestIdleShutdownDeferredWhileRequestInFlight(t *testing.T) {
	s := testServer(t)

	// Stale lastSeen + no in-flight work → idle.
	s.mu.Lock()
	s.lastSeen = time.Now().Add(-2 * idleTimeout)
	s.mu.Unlock()
	if !s.idleExpired() {
		t.Fatal("expected idle expiry with a stale lastSeen and no in-flight requests")
	}

	// An in-flight request suppresses idle shutdown no matter how stale lastSeen is.
	s.beginRequest()
	s.mu.Lock()
	s.lastSeen = time.Now().Add(-2 * idleTimeout)
	s.mu.Unlock()
	if s.idleExpired() {
		t.Fatal("idle expiry fired while a request was in flight — a long build would be killed")
	}

	// Finishing the request restarts the countdown from now.
	s.endRequest()
	if s.idleExpired() {
		t.Fatal("idle expired immediately after a request finished; countdown should restart")
	}
}

func TestQuitSignalsShutdown(t *testing.T) {
	s := testServer(t)
	h := s.routes()
	rec := do(h, "POST", "http://127.0.0.1:9999/api/quit", true, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("quit: got %d, want 200", rec.Code)
	}
	select {
	case <-s.quit:
		// closed as expected
	case <-time.After(time.Second):
		t.Error("expected quit channel to be closed")
	}
}
