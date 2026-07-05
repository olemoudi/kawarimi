package gui

import (
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/olemoudi/kawarimi/internal/selfupdate"
	"github.com/olemoudi/kawarimi/internal/testenv"
)

// lockedServer returns a server sharing the (already initialized) HOME but with a
// locked session, for exercising the unlock endpoint itself.
func lockedServer() *server {
	return &server{
		token: testToken, addr: "127.0.0.1:9999", port: "9999",
		opts: Options{Version: "test"}, sess: &session{}, lastSeen: time.Now(), quit: make(chan struct{}),
	}
}

func TestUnlockEndpoint(t *testing.T) {
	newUnlockedServer(t) // creates the vault in an isolated HOME
	s := lockedServer()
	h := s.routes()

	if rec := call(h, "POST", "/api/unlock", map[string]string{"password": "wrong-password"}); rec.Code != http.StatusBadRequest {
		t.Errorf("wrong password: got %d, want 400", rec.Code)
	}
	if s.sess.isUnlocked() {
		t.Fatal("a failed unlock must not leave the session unlocked")
	}

	rec := call(h, "POST", "/api/unlock", map[string]string{"password": testPassword})
	if rec.Code != http.StatusOK {
		t.Fatalf("unlock: got %d (%s)", rec.Code, rec.Body.String())
	}
	var st stateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil || !st.Unlocked {
		t.Errorf("unlock response should report unlocked, got %s", rec.Body.String())
	}

	if rec := call(h, "GET", "/api/ping", nil); rec.Code != http.StatusOK {
		t.Errorf("ping: got %d, want 200", rec.Code)
	}
	if rec := call(h, "GET", "/api/unlock", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET unlock: got %d, want 405", rec.Code)
	}
}

// TestPasswordStrengthEndpoint exercises the live-meter scoring endpoint.
func TestPasswordStrengthEndpoint(t *testing.T) {
	testenv.SetHome(t, t.TempDir())
	h := lockedServer().routes()

	rec := call(h, "POST", "/api/password-strength", map[string]string{"password": "password"})
	if rec.Code != http.StatusOK {
		t.Fatalf("strength: got %d (%s)", rec.Code, rec.Body.String())
	}
	var weak struct {
		Level    int     `json:"level"`
		LevelKey string  `json:"levelKey"`
		Bits     float64 `json:"bits"`
		Warning  string  `json:"warning"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &weak); err != nil {
		t.Fatal(err)
	}
	if weak.Level != 0 || weak.LevelKey != "very_weak" || weak.Warning != "common_password" {
		t.Errorf("'password' must score very_weak/common_password, got %+v", weak)
	}

	rec = call(h, "POST", "/api/password-strength", map[string]string{"password": "kX9$mQ2#vL5!pR8&"})
	var strong struct {
		Level int `json:"level"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &strong); err != nil {
		t.Fatal(err)
	}
	if strong.Level != 4 {
		t.Errorf("random 16-char must score excellent, got %d", strong.Level)
	}

	if rec := call(h, "GET", "/api/password-strength", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET strength: got %d, want 405", rec.Code)
	}
}

// TestInitWeakPasswordGate: the server refuses a weak vault password unless the
// client sends the explicit acceptWeak override (the SPA's checkbox).
func TestInitWeakPasswordGate(t *testing.T) {
	home := testenv.SetHome(t, t.TempDir())
	h := lockedServer().routes()

	rec := call(h, "POST", "/api/init", map[string]any{
		"password": "password", "vaultDir": filepath.Join(home, "vault"),
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("weak password without override: got %d, want 400", rec.Code)
	}
	if body := rec.Body.String(); !json.Valid([]byte(body)) || !strings.Contains(body, "weak_password") {
		t.Errorf("weak-password rejection must carry the weak_password key, got %s", body)
	}

	// With the override the init proceeds end to end (production KDF, one slow call).
	rec = call(h, "POST", "/api/init", map[string]any{
		"password": "password", "vaultDir": filepath.Join(home, "vault"), "acceptWeak": true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("weak password with override: got %d (%s)", rec.Code, rec.Body.String())
	}
}

// TestWizardFlow drives the browser wizard's server side exactly as the SPA does:
// init the vault, configure the switch, verify it, and check in.
func TestWizardFlow(t *testing.T) {
	home := testenv.SetHome(t, t.TempDir())
	s := lockedServer()
	h := s.routes()

	// Validation before any heavy work.
	if rec := call(h, "POST", "/api/init", map[string]string{"password": "  "}); rec.Code != http.StatusBadRequest {
		t.Errorf("blank password: got %d, want 400", rec.Code)
	}

	// Create the vault (production KDF: this is the one slow call in the flow).
	rec := call(h, "POST", "/api/init", map[string]string{
		"password": testPassword, "vaultDir": filepath.Join(home, "vault"),
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("init: got %d (%s)", rec.Code, rec.Body.String())
	}
	var initResp struct {
		VaultDir            string   `json:"vaultDir"`
		Mnemonic            []string `json:"mnemonic"`
		RecoveryCode        string   `json:"recoveryCode"`
		RecipientPassphrase string   `json:"recipientPassphrase"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &initResp); err != nil {
		t.Fatal(err)
	}
	if len(initResp.Mnemonic) == 0 || initResp.RecoveryCode == "" || initResp.RecipientPassphrase == "" {
		t.Fatalf("init must return the one-time secrets, got %s", rec.Body.String())
	}
	if !s.sess.isUnlocked() {
		t.Error("init should unlock the session for the wizard")
	}

	// Switch verify before setup must refuse.
	if rec := call(h, "POST", "/api/switch/verify", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("verify before switch setup: got %d, want 400", rec.Code)
	}

	// Configure the switch (no cloud step here).
	rec = call(h, "POST", "/api/switch/setup", map[string]any{
		"smtpServer": "smtp.test", "smtpUsername": "owner@test", "smtpPassword": "pw",
		"userEmail": "owner@test", "recipients": []string{"heir@test"},
		"vaultPackageLocation": "https://example.com/vault.zip",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("switch setup: got %d (%s)", rec.Code, rec.Body.String())
	}
	var setupResp struct {
		OK        bool `json:"ok"`
		CloudOnly bool `json:"cloudOnly"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &setupResp); err != nil || !setupResp.OK || !setupResp.CloudOnly {
		t.Errorf("switch setup should default to cloud-only, got %s", rec.Body.String())
	}

	// Verify now runs and reports the (cloud-less) switch state.
	rec = call(h, "POST", "/api/switch/verify", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("switch verify: got %d (%s)", rec.Code, rec.Body.String())
	}
	var verifyResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &verifyResp); err != nil {
		t.Fatal(err)
	}
	if _, ok := verifyResp["dmsConfigured"]; !ok {
		t.Errorf("verify response missing dmsConfigured: %s", rec.Body.String())
	}

	// Check in (no DMS remote yet: local write only).
	rec = call(h, "POST", "/api/checkin", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("checkin: got %d (%s)", rec.Code, rec.Body.String())
	}
	var checkinResp struct {
		OK     bool `json:"ok"`
		Pushed bool `json:"pushed"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &checkinResp); err != nil || !checkinResp.OK || checkinResp.Pushed {
		t.Errorf("checkin should succeed locally without pushing, got %s", rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(home, "vault", "last_checkin")); err != nil {
		t.Error("checkin did not write last_checkin")
	}

	// State reflects all of it.
	rec = call(h, "GET", "/api/state", nil)
	var st stateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if !st.Configured || !st.SwitchConfigured || !st.CloudOnly || st.LastCheckin == "" {
		t.Errorf("state after wizard = %+v", st)
	}
}

func TestWizardEndpointsRequireVault(t *testing.T) {
	testenv.SetHome(t, t.TempDir())
	h := lockedServer().routes()
	if rec := call(h, "POST", "/api/switch/setup", map[string]any{"smtpServer": "x"}); rec.Code != http.StatusBadRequest {
		t.Errorf("switch setup without a vault: got %d, want 400", rec.Code)
	}
	if rec := call(h, "POST", "/api/checkin", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("checkin without a vault: got %d, want 400", rec.Code)
	}
	if rec := call(h, "POST", "/api/switch/verify", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("verify without a vault: got %d, want 400", rec.Code)
	}
	if rec := call(h, "POST", "/api/package/build", map[string]string{"mode": "none"}); rec.Code != http.StatusBadRequest {
		t.Errorf("package build without a vault: got %d, want 400", rec.Code)
	}
}

func TestBuildSwitchConfigValidation(t *testing.T) {
	valid := switchSetupRequest{
		SMTPServer: "smtp.test", SMTPUsername: "o@t", UserEmail: "o@t",
		Recipients: []string{"h@t"}, VaultPackageLocation: "https://x/v.zip",
	}

	if sc, msg := buildSwitchConfig(valid); msg != "" {
		t.Fatalf("valid request rejected: %s", msg)
	} else {
		if sc.SenderEmail != "o@t" {
			t.Errorf("sender should default to the SMTP username, got %q", sc.SenderEmail)
		}
		if len(sc.PingChannels) != 1 || sc.PingChannels[0] != "email" {
			t.Errorf("ping channels = %v, want [email]", sc.PingChannels)
		}
	}

	tg := valid
	tg.TelegramBotToken = "tok"
	if sc, _ := buildSwitchConfig(tg); len(sc.PingChannels) != 2 || sc.PingChannels[1] != "telegram" {
		t.Error("a telegram token should add the telegram ping channel")
	}

	cases := []struct {
		name   string
		mutate func(*switchSetupRequest)
	}{
		{"no smtp", func(r *switchSetupRequest) { r.SMTPServer = "" }},
		{"no user email", func(r *switchSetupRequest) { r.UserEmail = "" }},
		{"no recipients", func(r *switchSetupRequest) { r.Recipients = []string{"  "} }},
		{"no package location", func(r *switchSetupRequest) { r.VaultPackageLocation = "" }},
		{"bad thresholds", func(r *switchSetupRequest) { r.Warning1Days, r.Warning2Days, r.FinalDays = 30, 20, 10 }},
	}
	for _, c := range cases {
		req := valid
		req.Recipients = append([]string(nil), valid.Recipients...)
		c.mutate(&req)
		if _, msg := buildSwitchConfig(req); msg == "" {
			t.Errorf("%s: expected a validation error", c.name)
		}
	}
}

// releaseServer serves a minimal GitHub /releases/latest for the update endpoints.
// No signing needed: signature checks belong to Apply, which is tested directly in
// internal/selfupdate (running it here would replace the test binary).
func releaseServer(t *testing.T, tag string) *httptest.Server {
	t.Helper()
	name := selfupdate.AssetName(runtime.GOOS, runtime.GOARCH)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"tag_name": tag,
			"html_url": "https://example.com/rel",
			"assets": []map[string]string{
				{"name": name, "browser_download_url": "http://unused/asset"},
				{"name": "checksums.txt", "browser_download_url": "http://unused/sums"},
				{"name": "checksums.txt.sig", "browser_download_url": "http://unused/sig"},
			},
		})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("KAWARIMI_GITHUB_API", srv.URL)
	return srv
}

func TestUpdateCheckLiveThenCached(t *testing.T) {
	testenv.SetHome(t, t.TempDir())
	srv := releaseServer(t, "v9.9.9")
	s := lockedServer()
	s.opts.Version = "0.1.0"
	h := s.routes()

	rec := call(h, "GET", "/api/update/check", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("update check: got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Available bool   `json:"available"`
		Version   string `json:"version"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || !resp.Available || resp.Version != "9.9.9" {
		t.Fatalf("live check = %s", rec.Body.String())
	}

	// Kill the mock: a fresh cache must answer without any network.
	srv.Close()
	rec = call(h, "GET", "/api/update/check", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("cached check: got %d", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || !resp.Available || resp.Version != "9.9.9" {
		t.Errorf("cached check = %s, want the cached release", rec.Body.String())
	}
}

func TestUpdateApplyUpToDateAndOffline(t *testing.T) {
	testenv.SetHome(t, t.TempDir())
	releaseServer(t, "v0.0.1") // older than the running version
	s := lockedServer()
	s.opts.Version = "1.0.0"
	h := s.routes()

	rec := call(h, "POST", "/api/update/apply", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("apply when current: got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp["upToDate"] != true {
		t.Errorf("apply when current = %s, want upToDate", rec.Body.String())
	}

	// An unreachable release API is a 502, not a hang or a silent success.
	fail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer fail.Close()
	t.Setenv("KAWARIMI_GITHUB_API", fail.URL)
	if rec := call(h, "POST", "/api/update/apply", nil); rec.Code != http.StatusBadGateway {
		t.Errorf("apply with a failing API: got %d, want 502", rec.Code)
	}
}

func TestPackageBuildWithoutBinaries(t *testing.T) {
	s := newUnlockedServer(t)
	h := s.routes()
	out := filepath.Join(t.TempDir(), "pkg.zip")

	rec := call(h, "POST", "/api/package/build", map[string]string{"mode": "none", "output": out})
	if rec.Code != http.StatusOK {
		t.Fatalf("package build: got %d (%s)", rec.Code, rec.Body.String())
	}
	info, err := os.Stat(out)
	if err != nil || info.Size() == 0 {
		t.Fatalf("package zip missing or empty: %v", err)
	}
	if !strings.Contains(rec.Body.String(), `"binariesSource":"none"`) {
		t.Errorf("response must report the binaries source, got %s", rec.Body.String())
	}
}

func TestPackageBuildNeedsSourceForBinaries(t *testing.T) {
	// Kill the official-binaries fetch (no network in tests): auto mode must fall
	// back to a local build, and with no source checkout either, explain both.
	t.Setenv("KAWARIMI_GITHUB_API", "http://127.0.0.1:1")
	s := newUnlockedServer(t)
	s.opts.SourceDir = "" // and the test cwd (internal/gui) is not a module root
	h := s.routes()
	rec := call(h, "POST", "/api/package/build", map[string]string{"mode": "auto"})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("auto mode without source: got %d (%s), want 400", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "official release binaries") {
		t.Errorf("the error should mention the failed official fetch, got %s", body)
	}
}

func TestResolveSourceDir(t *testing.T) {
	dir := t.TempDir()
	if isKawarimiModule(dir) {
		t.Error("an empty dir is not the kawarimi module")
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/olemoudi/kawarimi\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if !isKawarimiModule(dir) {
		t.Error("a dir with the kawarimi go.mod must be recognized")
	}
	s := lockedServer()
	s.opts.SourceDir = dir
	if got := s.resolveSourceDir(); got != dir {
		t.Errorf("resolveSourceDir = %q, want the explicit --source dir", got)
	}
}

func TestRandomToken(t *testing.T) {
	a, err := randomToken()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := randomToken()
	if len(a) != 64 || a == b {
		t.Errorf("tokens must be 256-bit and unique: %q %q", a, b)
	}
	if _, err := hex.DecodeString(a); err != nil {
		t.Errorf("token is not hex: %v", err)
	}
}

// TestServerLifecycle serves on a real loopback listener and shuts down via the
// quit path, covering watchLifecycle + shutdown.
func TestServerLifecycle(t *testing.T) {
	testenv.SetHome(t, t.TempDir())
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := lockedServer()
	s.httpSrv = &http.Server{Handler: s.routes()}

	done := make(chan error, 1)
	go func() { done <- s.httpSrv.Serve(ln) }()
	go s.watchLifecycle()

	s.touch() // exercise the keepalive path
	s.requestQuit()
	s.requestQuit() // idempotent

	select {
	case err := <-done:
		if err != http.ErrServerClosed {
			t.Errorf("Serve returned %v, want ErrServerClosed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down after quit")
	}
}
