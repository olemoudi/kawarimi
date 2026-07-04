// Package gui serves the kawarimi owner app as a local web page: a pure-Go
// net/http server bound to loopback that opens in the user's browser. It reuses the
// same orchestration (internal/setup), vault, deadswitch, and github packages the
// CLI uses. See auth.go for the security model (loopback-only, per-session token,
// Host/Origin allowlist, strict CSP, auto-shutdown).
package gui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// idleTimeout stops the server after this long with no request. The SPA sends a
// keepalive ping well within this window, so the server lives while the tab is open
// and shuts down shortly after it closes.
const idleTimeout = 90 * time.Second

// Options configure the GUI server.
type Options struct {
	Port      int    // 0 = random ephemeral port
	NoBrowser bool   // do not auto-open the browser
	Version   string // stamped build version, shown in the UI and used for packaging
	SourceDir string // kawarimi source checkout for package cross-compile ("" = auto)
}

type server struct {
	token   string
	addr    string // 127.0.0.1:<port>
	port    string
	opts    Options
	sess    *session
	httpSrv *http.Server

	mu       sync.Mutex
	lastSeen time.Time
	inflight int // requests currently being served; idle shutdown is deferred while > 0
	quitOnce sync.Once
	quit     chan struct{}
}

// Run starts the GUI server and blocks until it shuts down (idle, quit, or signal).
func Run(opts Options) error {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", opts.Port))
	if err != nil {
		return fmt.Errorf("listening on loopback: %w", err)
	}
	port := fmt.Sprintf("%d", ln.Addr().(*net.TCPAddr).Port)

	token, err := randomToken()
	if err != nil {
		return err
	}

	s := &server{
		token:    token,
		addr:     "127.0.0.1:" + port,
		port:     port,
		opts:     opts,
		sess:     &session{},
		lastSeen: time.Now(),
		quit:     make(chan struct{}),
	}
	s.httpSrv = &http.Server{Handler: s.routes()}

	url := fmt.Sprintf("http://%s/?t=%s", s.addr, token)
	fmt.Printf("\nkawarimi GUI is running at:\n  %s\n\n", url)
	fmt.Println("Keep this terminal open. Use \"Quit\" in the page or press Ctrl-C to stop.")
	if !opts.NoBrowser {
		if err := openBrowser(url); err != nil {
			fmt.Printf("(couldn't open a browser automatically: %v — open the URL above)\n", err)
		}
	}

	go s.watchLifecycle()

	if err := s.httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// randomToken returns a 256-bit hex session token.
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating session token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// touch records activity, used by the idle watchdog.
func (s *server) touch() {
	s.mu.Lock()
	s.lastSeen = time.Now()
	s.mu.Unlock()
}

// beginRequest/endRequest bracket every request so the idle watchdog never shuts
// the server down while work is in flight — a package build (cross-compile) can
// exceed the idle timeout, and browsers throttle background-tab keepalives, so
// counting in-flight work is the only reliable guard.
func (s *server) beginRequest() {
	s.mu.Lock()
	s.inflight++
	s.lastSeen = time.Now()
	s.mu.Unlock()
}

func (s *server) endRequest() {
	s.mu.Lock()
	s.inflight--
	s.lastSeen = time.Now() // idle countdown restarts when the work finishes
	s.mu.Unlock()
}

// idleExpired reports whether the server has been idle past the timeout with no
// requests in flight.
func (s *server) idleExpired() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inflight == 0 && time.Since(s.lastSeen) > idleTimeout
}

// requestQuit triggers a graceful shutdown (idempotent).
func (s *server) requestQuit() {
	s.quitOnce.Do(func() { close(s.quit) })
}

// watchLifecycle shuts the server down on an explicit quit, on OS signals, or after
// the idle timeout elapses with no requests.
func (s *server) watchLifecycle() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.quit:
			s.shutdown("requested")
			return
		case <-sigCh:
			s.shutdown("signal")
			return
		case <-ticker.C:
			if s.idleExpired() {
				s.shutdown("idle")
				return
			}
		}
	}
}

func (s *server) shutdown(reason string) {
	fmt.Printf("\nkawarimi GUI shutting down (%s).\n", reason)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = s.httpSrv.Shutdown(ctx)
}
