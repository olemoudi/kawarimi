package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/olemoudi/kawarimi/internal/demo"
)

// newDemoServer builds a demo world (local engine, so it runs on any OS) and a
// GUI server wired to it, exactly as `kawarimi demo` does.
func newDemoServer(t *testing.T) (*server, *demo.World) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("KAWARIMI_TELEGRAM_API", "")
	t.Setenv("KAWARIMI_GITHUB_API", "")

	w, err := demo.NewWorld(demo.Options{ForceLocalEngine: true, Version: "test"})
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	t.Cleanup(func() { w.Close() })

	s := &server{
		token: testToken, addr: "127.0.0.1:9999", port: "9999",
		opts: Options{Version: "test", Demo: w}, sess: &session{}, lastSeen: time.Now(), quit: make(chan struct{}),
	}
	if err := s.sess.unlock(w.Password()); err != nil {
		t.Fatalf("demo auto-unlock: %v", err)
	}
	return s, w
}

// TestDemoLifecycleClickThrough drives the demo API the way a user clicks through
// the theater: silence → warning → check-in → release → recipient opens → reset.
func TestDemoLifecycleClickThrough(t *testing.T) {
	s, _ := newDemoServer(t)
	h := s.routes()

	// The SPA routes into the demo view off these flags.
	rec := call(h, "GET", "/api/state", nil)
	var st stateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if !st.DemoMode || !st.Configured || !st.Unlocked || st.EntryCount != 3 {
		t.Fatalf("demo /api/state: %+v", st)
	}

	snap := demoCall(t, h, "GET", "/api/demo/state", nil)
	if snap.Day != 0 || snap.KeyB64 != "" || snap.CardWords == "" {
		t.Fatalf("fresh demo snapshot: day=%d key=%q card=%q", snap.Day, snap.KeyB64, snap.CardWords)
	}

	// API contract: list fields are JSON arrays even when empty, never null —
	// the SPA iterates them and a null crashed the very first page load once.
	raw := call(h, "GET", "/api/demo/state", nil).Body.String()
	for _, field := range []string{"phone", "events", "ownerInbox", "recipientInbox", "cron"} {
		if strings.Contains(raw, `"`+field+`":null`) {
			t.Errorf("fresh snapshot marshals %q as null; must be an empty array:\n%s", field, raw)
		}
	}

	// Quiet days, then a warning.
	snap = demoCall(t, h, "POST", "/api/demo/advance", map[string]int{"days": 15})
	if snap.Stage != "warning1" || len(snap.OwnerInbox) == 0 || len(snap.Phone) == 0 {
		t.Fatalf("day 15: stage=%s mails=%d pings=%d", snap.Stage, len(snap.OwnerInbox), len(snap.Phone))
	}

	// The owner proves life.
	snap = demoCall(t, h, "POST", "/api/demo/checkin", nil)
	if snap.Day != 0 {
		t.Fatalf("after checkin: day=%d", snap.Day)
	}

	// A Telegram /alive also counts as proof of life on the next daily tick.
	demoCall(t, h, "POST", "/api/demo/advance", map[string]int{"days": 5})
	demoCall(t, h, "POST", "/api/demo/telegram-alive", nil)
	snap = demoCall(t, h, "POST", "/api/demo/advance", map[string]int{"days": 1})
	if snap.Day != 0 {
		t.Fatalf("after /alive reply: day=%d, want auto-checkin to 0", snap.Day)
	}

	// Then the owner goes silent for good.
	snap = demoCall(t, h, "POST", "/api/demo/advance", map[string]int{"days": 32})
	if !snap.Released || snap.KeyB64 == "" || len(snap.RecipientInbox) == 0 {
		t.Fatalf("day 32: released=%v key=%q recipientMails=%d", snap.Released, snap.KeyB64, len(snap.RecipientInbox))
	}

	// The recipient: a wrong attempt fails with a friendly 400, the real one opens.
	if rec := call(h, "POST", "/api/demo/recipient-open", map[string]string{
		"key": snap.KeyB64, "words": "abandon abandon abandon abandon abandon abandon",
	}); rec.Code != http.StatusBadRequest {
		t.Fatalf("wrong words: got %d, want 400", rec.Code)
	}
	snap = demoCall(t, h, "POST", "/api/demo/recipient-open", map[string]string{
		"key": snap.KeyB64, "words": snap.CardWords,
	})
	if !snap.Recipient.Opened || snap.Recipient.Index == "" {
		t.Fatalf("recipient open: %+v", snap.Recipient)
	}

	// Reset rewinds everything with fresh secrets.
	old := snap.CardWords
	snap = demoCall(t, h, "POST", "/api/demo/reset", nil)
	if snap.Day != 0 || snap.Released || snap.CardWords == old {
		t.Fatalf("after reset: %+v", snap)
	}

	// Bad input surfaces as 400, not 500.
	if rec := call(h, "POST", "/api/demo/advance", map[string]int{"days": 0}); rec.Code != http.StatusBadRequest {
		t.Errorf("advance 0 days: got %d, want 400", rec.Code)
	}
}

// demoCall hits a demo endpoint and decodes the snapshot.
func demoCall(t *testing.T, h http.Handler, method, path string, body any) *demo.Snapshot {
	t.Helper()
	rec := call(h, method, path, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s %s: %d (%s)", method, path, rec.Code, rec.Body.String())
	}
	var snap demo.Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatal(err)
	}
	return &snap
}

// The demo surface must not exist outside demo mode, and must stay session-gated
// inside it.
func TestDemoEndpointsGating(t *testing.T) {
	// A regular (non-demo) server: 404 on every demo route.
	newUnlockedServer(t)
	plain := lockedServer()
	h := plain.routes()
	for _, route := range []string{"/api/demo/state", "/api/demo/advance", "/api/demo/reset"} {
		method := "POST"
		if route == "/api/demo/state" {
			method = "GET"
		}
		if rec := call(h, method, route, nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s on a non-demo server: got %d, want 404", route, rec.Code)
		}
	}

	// A demo server without the session cookie: 403.
	s, _ := newDemoServer(t)
	dh := s.routes()
	req := httptest.NewRequest("GET", "http://127.0.0.1:9999/api/demo/state", nil)
	rec := httptest.NewRecorder()
	dh.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("uncookied demo state: got %d, want 403", rec.Code)
	}
}
