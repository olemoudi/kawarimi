package gui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/olemoudi/kawarimi/internal/demo"
	"github.com/olemoudi/kawarimi/internal/testenv"
)

// These smokes execute the embedded SPA's JavaScript for real: a headless browser
// loads each view from a live server and the test fails on ANY uncaught exception
// or console.error. They exist because source-pinning tests cannot see runtime JS
// errors — a null where the SPA expected an array once crashed the demo's very
// first paint and nothing in `go test` noticed. Gated on an installed browser
// (testenv.RequireBrowser), like the workflow runner is gated on linux+bash.

// startSmokeServer runs a real GUI server on a loopback socket and returns its
// tokenized URL.
func startSmokeServer(t *testing.T, opts Options) (*server, string) {
	t.Helper()
	s, ln, url, err := newGUIServer(opts)
	if err != nil {
		t.Fatalf("newGUIServer: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- s.serve(ln) }()
	t.Cleanup(func() {
		s.requestQuit()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("smoke server did not shut down")
		}
	})
	return s, url
}

// smokeLoad navigates the browser to url, waits for readyExpr to become truthy,
// and fails on any JS error seen along the way.
func smokeLoad(t *testing.T, url, readyExpr string) (ctx context.Context, jsErrors func() []string) {
	t.Helper()
	bctx := testenv.NewBrowser(t)
	getErrors := testenv.WatchJSErrors(bctx)
	if err := chromedp.Run(bctx, chromedp.Navigate(url)); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if err := testenv.WaitTruthy(bctx, readyExpr); err != nil {
		t.Fatalf("%v\nJS errors so far: %v", err, getErrors())
	}
	return bctx, getErrors
}

func assertNoJSErrors(t *testing.T, view string, jsErrors func() []string) {
	t.Helper()
	if errs := jsErrors(); len(errs) > 0 {
		t.Fatalf("%s view produced JS errors:\n%s", view, errs)
	}
}

// The first-run wizard (nothing configured) must paint without JS errors.
func TestBrowserSmokeWizard(t *testing.T) {
	testenv.RequireBrowser(t)
	testenv.SetHome(t, t.TempDir())
	_, url := startSmokeServer(t, Options{Version: "test", NoBrowser: true})

	_, jsErrors := smokeLoad(t, url, `document.getElementById("wpw")`)
	assertNoJSErrors(t, "wizard", jsErrors)
}

// The unlock view (configured, locked) must paint without JS errors.
func TestBrowserSmokeUnlock(t *testing.T) {
	testenv.RequireBrowser(t)
	env := testenv.New(t)
	env.InitVault(t)
	_, url := startSmokeServer(t, Options{Version: "test", NoBrowser: true})

	_, jsErrors := smokeLoad(t, url, `document.getElementById("pw")`)
	assertNoJSErrors(t, "unlock", jsErrors)
}

// Dashboard and entries (configured, unlocked) must paint without JS errors.
func TestBrowserSmokeDashboardAndEntries(t *testing.T) {
	testenv.RequireBrowser(t)
	env := testenv.New(t)
	env.InitVault(t)
	s, url := startSmokeServer(t, Options{Version: "test", NoBrowser: true})
	if err := s.sess.unlock(env.Password()); err != nil {
		t.Fatalf("unlock: %v", err)
	}

	bctx, jsErrors := smokeLoad(t, url, `document.querySelector(".facts")`)
	assertNoJSErrors(t, "dashboard", jsErrors)

	// Entries via the real nav tab (the SPA keeps the session cookie).
	if err := chromedp.Run(bctx, chromedp.Click(`.navtab:nth-child(2)`, chromedp.ByQuery)); err != nil {
		t.Fatalf("click entries tab: %v", err)
	}
	if err := testenv.WaitTruthy(bctx, `document.querySelector(".empty-state") || document.querySelector(".entry-list")`); err != nil {
		t.Fatalf("%v\nJS errors: %v", err, jsErrors())
	}
	assertNoJSErrors(t, "entries", jsErrors)
}

// The demo theater must paint at PRISTINE day 0 (the state that crashed once) and
// survive real interaction: advancing a week via the actual button.
func TestBrowserSmokeDemoTheater(t *testing.T) {
	testenv.RequireBrowser(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("KAWARIMI_TELEGRAM_API", "")
	t.Setenv("KAWARIMI_GITHUB_API", "")

	w, err := demo.NewWorld(demo.Options{ForceLocalEngine: true, Version: "test"})
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	t.Cleanup(func() { w.Close() })
	_, url := startSmokeServer(t, Options{Version: "test", NoBrowser: true, Demo: w})

	// Day 0: four columns, empty inboxes, no JS errors — the regression case.
	bctx, jsErrors := smokeLoad(t, url,
		`document.querySelector(".demo-grid") && document.querySelector(".demo-day")`)
	assertNoJSErrors(t, "demo day 0", jsErrors)

	var day0 string
	if err := chromedp.Run(bctx, chromedp.Text(".demo-day", &day0, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if day0 != fmt.Sprintf("Day %d", 0) && day0 != "Día 0" {
		t.Fatalf("fresh theater shows %q, want day 0", day0)
	}

	// Click "+1 day" (the first control button) for real and wait for the story to move.
	if err := chromedp.Run(bctx, chromedp.Click(`.demo-controls button`, chromedp.ByQuery)); err != nil {
		t.Fatalf("click advance: %v", err)
	}
	if err := testenv.WaitTruthy(bctx,
		`document.querySelector(".demo-day") && /(Day|Día) [1-9]/.test(document.querySelector(".demo-day").textContent)`); err != nil {
		t.Fatalf("%v\nJS errors: %v", err, jsErrors())
	}
	assertNoJSErrors(t, "demo after advancing", jsErrors)
}

// The smoke suite is only as good as its error detector: prove WatchJSErrors
// actually catches both console.error and uncaught exceptions.
func TestBrowserSmokeHarnessDetectsErrors(t *testing.T) {
	testenv.RequireBrowser(t)
	testenv.SetHome(t, t.TempDir())
	_, url := startSmokeServer(t, Options{Version: "test", NoBrowser: true})

	bctx, jsErrors := smokeLoad(t, url, `document.getElementById("wpw")`)
	err := chromedp.Run(bctx, chromedp.Evaluate(
		`console.error("detector-check"); setTimeout(() => { throw new Error("detector-throw"); }, 0); true`, nil))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		errs := jsErrors()
		var sawConsole, sawThrow bool
		for _, e := range errs {
			if strings.Contains(e, "detector-check") {
				sawConsole = true
			}
			if strings.Contains(e, "detector-throw") {
				sawThrow = true
			}
		}
		if sawConsole && sawThrow {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("detector missed injected errors; saw: %v", errs)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
