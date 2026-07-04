package gui

import (
	"crypto/subtle"
	_ "embed"
	"net/http"
)

const cookieName = "kawarimi_session"

//go:embed web/index.html
var indexHTML []byte

//go:embed web/app.js
var appJS []byte

//go:embed web/app.css
var appCSS []byte

// routes wires the mux and wraps it in the security middleware.
func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/app.js", s.requireSession(s.static(appJS, "text/javascript; charset=utf-8")))
	mux.HandleFunc("/app.css", s.requireSession(s.static(appCSS, "text/css; charset=utf-8")))

	// API
	mux.HandleFunc("/api/state", s.requireSession(s.handleState))
	mux.HandleFunc("/api/ping", s.requireSession(s.handlePing))
	mux.HandleFunc("/api/unlock", s.requireSession(s.handleUnlock))
	mux.HandleFunc("/api/quit", s.requireSession(s.handleQuit))
	mux.HandleFunc("/api/checkin", s.requireSession(s.handleCheckin))
	mux.HandleFunc("/api/switch/verify", s.requireSession(s.handleSwitchVerify))

	// Setup wizard
	mux.HandleFunc("/api/init", s.requireSession(s.handleInit))
	mux.HandleFunc("/api/switch/setup", s.requireSession(s.handleSwitchSetup))
	mux.HandleFunc("/api/switch/cloud", s.requireSession(s.handleSwitchCloud))
	mux.HandleFunc("/api/package/build", s.requireSession(s.handlePackageBuild))

	// Entries (method-routed; {id} via r.PathValue)
	mux.HandleFunc("GET /api/entries", s.requireSession(s.handleEntriesList))
	mux.HandleFunc("POST /api/entries", s.requireSession(s.handleEntryCreate))
	mux.HandleFunc("GET /api/entries/{id}", s.requireSession(s.handleEntryGet))
	mux.HandleFunc("PUT /api/entries/{id}", s.requireSession(s.handleEntryUpdate))
	mux.HandleFunc("DELETE /api/entries/{id}", s.requireSession(s.handleEntryDelete))

	return s.withSecurity(mux)
}

// withSecurity applies to every request: it enforces the loopback Host allowlist
// (DNS-rebinding defense), sets a strict CSP and related headers, and records
// activity for the idle watchdog.
func (s *server) withSecurity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.hostAllowed(r.Host) {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		h := w.Header()
		h.Set("Content-Security-Policy",
			"default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
				"connect-src 'self'; img-src 'self' data:; base-uri 'none'; form-action 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		s.touch()
		next.ServeHTTP(w, r)
	})
}

// hostAllowed accepts only 127.0.0.1:<port> and localhost:<port>.
func (s *server) hostAllowed(host string) bool {
	return host == "127.0.0.1:"+s.port || host == "localhost:"+s.port
}

func (s *server) validToken(tok string) bool {
	return subtle.ConstantTimeCompare([]byte(tok), []byte(s.token)) == 1
}

func (s *server) hasValidCookie(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	return err == nil && s.validToken(c.Value)
}

// originAllowed guards state-changing requests: an Origin header, if present, must
// be one of our loopback origins. (SameSite=Strict already blocks the cookie
// cross-site; this is defense in depth.)
func (s *server) originAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	return origin == "http://127.0.0.1:"+s.port || origin == "http://localhost:"+s.port
}

// requireSession gates a handler behind the per-session cookie, and behind the
// Origin allowlist for mutating methods.
func (s *server) requireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.hasValidCookie(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !s.originAllowed(r) {
			http.Error(w, "forbidden origin", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

// handleIndex bootstraps the session: a valid ?t=<token> sets the session cookie
// and redirects to a clean URL; thereafter a valid cookie serves the SPA.
func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if tok := r.URL.Query().Get("t"); tok != "" && s.validToken(tok) {
		http.SetCookie(w, &http.Cookie{
			Name:     cookieName,
			Value:    s.token,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
		})
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	if !s.hasValidCookie(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(indexHTML)
}

// static serves an embedded asset.
func (s *server) static(content []byte, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(content)
	}
}
