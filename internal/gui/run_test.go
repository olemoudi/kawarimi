package gui

import (
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"

	"github.com/olemoudi/kawarimi/internal/testenv"
)

// TestGUIServerLifecycleOverSocket drives the launch path the way `kawarimi gui`
// does — real listener, tokenized URL, cookie bootstrap — and shuts it down via
// the Quit API, proving the whole lifecycle needs no browser and no human.
func TestGUIServerLifecycleOverSocket(t *testing.T) {
	testenv.SetHome(t, t.TempDir())

	s, ln, url, err := newGUIServer(Options{NoBrowser: true, Version: "test"})
	if err != nil {
		t.Fatalf("newGUIServer: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- s.serve(ln) }()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second}

	// Bootstrap: the tokenized URL exchanges ?t= for the session cookie and
	// serves the SPA on the clean URL.
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("bootstrap GET: %v", err)
	}
	body := make([]byte, 512)
	n, _ := resp.Body.Read(body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body[:n]), "<") {
		t.Fatalf("bootstrap: status %d, body %q", resp.StatusCode, body[:n])
	}

	// The cookie now authorizes the API.
	base := "http://" + s.addr
	resp, err = client.Get(base + "/api/state")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/api/state with session cookie: %d", resp.StatusCode)
	}

	// A cookie-less client is refused (the token URL was consumed by redirect).
	plain := &http.Client{Timeout: 5 * time.Second}
	resp, err = plain.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("no-cookie GET /: %d, want 403", resp.StatusCode)
	}

	// Quit via the API: serve() must return promptly.
	req, err := http.NewRequest(http.MethodPost, base+"/api/quit", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", base)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/api/quit: %d", resp.StatusCode)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("server did not shut down after quit")
	}
}
