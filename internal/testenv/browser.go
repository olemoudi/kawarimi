package testenv

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// This file gates and drives a real headless browser so tests can execute the
// embedded SPA's JavaScript — the one part of the product `go test` cannot reach
// otherwise (the generated workflow's bash gets the mini Actions runner; app.js
// gets this). Tests skip when no Chrome/Chromium is installed, mirroring
// RequireWorkflowRunner; GitHub's ubuntu runners ship Chrome, so CI executes them.

// initialHome is the real home dir, captured before any test isolates HOME with
// t.Setenv — browser discovery must keep working from inside a sandboxed test.
var initialHome, _ = os.UserHomeDir()

// FindBrowser returns a Chrome/Chromium binary suitable for DevTools driving, or
// "" if none is installed. KAWARIMI_CHROME overrides discovery.
func FindBrowser() string {
	if v := os.Getenv("KAWARIMI_CHROME"); v != "" {
		return v
	}
	for _, name := range []string{
		"google-chrome-stable", "google-chrome", "chromium", "chromium-browser", "chrome",
	} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	// Playwright's cached browsers (common on dev machines).
	homes := []string{initialHome}
	if h, err := os.UserHomeDir(); err == nil && h != initialHome {
		homes = append(homes, h)
	}
	for _, home := range homes {
		for _, pattern := range []string{
			filepath.Join(home, ".cache", "ms-playwright", "chromium-*", "chrome-linux64", "chrome"),
			filepath.Join(home, ".cache", "ms-playwright", "chromium_headless_shell-*", "chrome-headless-shell-linux64", "chrome-headless-shell"),
		} {
			if matches, _ := filepath.Glob(pattern); len(matches) > 0 {
				return matches[len(matches)-1] // highest version last
			}
		}
	}
	return ""
}

// RequireBrowser skips the test unless a drivable browser is installed.
func RequireBrowser(t testing.TB) string {
	t.Helper()
	browser := FindBrowser()
	if browser == "" {
		t.Skip("browser smoke needs Chrome/Chromium (set KAWARIMI_CHROME to override discovery)")
	}
	return browser
}

// NewBrowser returns a chromedp context driving a fresh headless browser, cleaned
// up with the test. The overall budget bounds a hung browser, not a slow test.
func NewBrowser(t testing.TB) context.Context {
	t.Helper()
	browser := RequireBrowser(t)

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(browser),
		chromedp.NoSandbox,
		chromedp.DisableGPU,
	)
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, cancelCtx := chromedp.NewContext(allocCtx)
	ctx, cancelTimeout := context.WithTimeout(ctx, 120*time.Second)
	t.Cleanup(func() {
		cancelTimeout()
		cancelCtx()
		cancelAlloc()
	})
	return ctx
}

// WatchJSErrors collects uncaught exceptions and console.error calls from the
// page; the returned getter reports everything seen so far. Register it BEFORE
// navigating.
func WatchJSErrors(ctx context.Context) func() []string {
	var errs []string
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		switch e := ev.(type) {
		case *runtime.EventExceptionThrown:
			errs = append(errs, "uncaught exception: "+e.ExceptionDetails.Error())
		case *runtime.EventConsoleAPICalled:
			if e.Type != runtime.APITypeError {
				return
			}
			var parts []string
			for _, arg := range e.Args {
				if arg.Value != nil {
					parts = append(parts, string(arg.Value))
				} else if arg.Description != "" {
					parts = append(parts, arg.Description)
				}
			}
			errs = append(errs, "console.error: "+strings.Join(parts, " "))
		}
	})
	return func() []string { return append([]string(nil), errs...) }
}

// WaitTruthy polls a JS expression until it evaluates truthy (30s budget) —
// gentler than brittle sleeps for "the SPA finished rendering X".
func WaitTruthy(ctx context.Context, expr string) error {
	deadline := time.Now().Add(30 * time.Second)
	for {
		var ok bool
		if err := chromedp.Run(ctx, chromedp.Evaluate("!!("+expr+")", &ok)); err != nil {
			return err
		}
		if ok {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("condition never became truthy: %s", expr)
		}
		time.Sleep(150 * time.Millisecond)
	}
}
