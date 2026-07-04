package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/olemoudi/kawarimi/internal/deadswitch"
	"github.com/olemoudi/kawarimi/internal/selfupdate"
	"github.com/olemoudi/kawarimi/internal/testenv"
	"github.com/olemoudi/kawarimi/internal/vault"
)

// withStdin replaces os.Stdin with a pipe carrying script for the duration of the
// test. Prompts (PromptPassphrase falls back to line reads off a non-tty stdin,
// fmt.Scan, bufio readers) all consume from it in order.
func withStdin(t *testing.T, script string) {
	t.Helper()
	w := stdinWriter(t)
	go func() {
		io.WriteString(w, script)
		w.Close()
	}()
}

// stdinWriter swaps os.Stdin for a pipe and returns the write end, for tests that
// need to feed input in phases (a buffered reader must not swallow later lines).
func stdinWriter(t *testing.T) *os.File {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = old
		r.Close()
		w.Close()
	})
	return w
}

// capture redirects *f (os.Stdout or os.Stderr) while fn runs and returns what
// was written.
func capture(t *testing.T, f **os.File, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := *f
	*f = w
	outCh := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		outCh <- string(b)
	}()
	defer func() {
		*f = old
	}()
	fn()
	w.Close()
	*f = old
	return <-outCh
}

// The CLI unlock matrix: the same vault must open through every owner slot the CLI
// offers — device key, recovery code, and mnemonic — and refuse a wrong password.
func TestOpenVaultDeviceKeySlot(t *testing.T) {
	env := testenv.New(t)
	env.InitVault(t)

	withStdin(t, env.Password()+"\n")
	v, err := openVault()
	if err != nil {
		t.Fatalf("openVault with device key: %v", err)
	}
	if v.Manifest == nil {
		t.Error("opened vault has no manifest")
	}
}

func TestOpenVaultWrongPasswordFails(t *testing.T) {
	env := testenv.New(t)
	env.InitVault(t)

	withStdin(t, "definitely-wrong\n")
	if _, err := openVault(); err == nil {
		t.Fatal("a wrong password must not open the vault")
	}
}

func TestOpenVaultRecoverySlot(t *testing.T) {
	env := testenv.New(t)
	secrets := env.InitVault(t)
	if err := os.Remove(filepath.Join(env.AppDir, "device.key")); err != nil {
		t.Fatal(err)
	}

	// choice "1" (recovery), then password, then the displayed recovery code.
	withStdin(t, "1\n"+env.Password()+"\n"+secrets.RecoveryCode+"\n")
	if _, err := openVault(); err != nil {
		t.Fatalf("openVault with recovery code: %v", err)
	}
}

func TestOpenVaultMnemonicSlot(t *testing.T) {
	env := testenv.New(t)
	secrets := env.InitVault(t)
	if err := os.Remove(filepath.Join(env.AppDir, "device.key")); err != nil {
		t.Fatal(err)
	}

	withStdin(t, "2\n"+strings.Join(secrets.MnemonicWords, " ")+"\n")
	if _, err := openVault(); err != nil {
		t.Fatalf("openVault with mnemonic: %v", err)
	}
}

// TestExportSealedV4 exercises the recipient-side CLI flow end to end: DMS key +
// card passphrase open the sealed vault, exactly as after a real release email.
func TestExportSealedV4(t *testing.T) {
	env := testenv.New(t)
	secrets := env.InitVault(t)
	dmsKeyB64, err := os.ReadFile(filepath.Join(env.AppDir, "dms-key"))
	if err != nil {
		t.Fatalf("no dms-key after init: %v", err)
	}

	oldVaultPath := vaultPath
	vaultPath = env.VaultDir
	t.Cleanup(func() { vaultPath = oldVaultPath })

	// exportWithDMSKey reads the key through a buffered reader, then the passphrase
	// straight from stdin — feed the lines in phases so buffering cannot steal the
	// second one (a real terminal delivers one line per read).
	w := stdinWriter(t)
	go func() {
		io.WriteString(w, strings.TrimSpace(string(dmsKeyB64))+"\n")
		time.Sleep(300 * time.Millisecond)
		io.WriteString(w, secrets.RecipientPassphrase+"\n")
		w.Close()
	}()

	v, err := exportWithSealedPayload()
	if err != nil {
		t.Fatalf("sealed export: %v", err)
	}
	outDir := filepath.Join(t.TempDir(), "decrypted")
	if err := v.Export(outDir); err != nil {
		t.Fatalf("export to dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "INDEX.md")); err != nil {
		t.Error("export did not write INDEX.md")
	}
}

func TestExportMnemonicMode(t *testing.T) {
	env := testenv.New(t)
	secrets := env.InitVault(t)

	oldVaultPath := vaultPath
	vaultPath = env.VaultDir
	t.Cleanup(func() { vaultPath = oldVaultPath })

	withStdin(t, strings.Join(secrets.MnemonicWords, " ")+"\n")
	if _, err := exportWithMnemonic(); err != nil {
		t.Fatalf("mnemonic export: %v", err)
	}
}

func TestResolveVaultDir(t *testing.T) {
	// Explicit flag wins.
	oldVaultPath := vaultPath
	vaultPath = "/explicit/path"
	t.Cleanup(func() { vaultPath = oldVaultPath })
	if dir, err := resolveVaultDir(); err != nil || dir != "/explicit/path" {
		t.Errorf("explicit --vault: dir=%q err=%v", dir, err)
	}
	vaultPath = ""

	// A vault/ subdir next to the cwd (extracted package) is auto-detected.
	pkgDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(pkgDir, "vault"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "vault", vault.HeaderFile), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	oldWD, _ := os.Getwd()
	if err := os.Chdir(pkgDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(oldWD) })
	testenv.SetHome(t, t.TempDir()) // no config fallback available
	if dir, err := resolveVaultDir(); err != nil || dir != "vault" {
		t.Errorf("package auto-detect: dir=%q err=%v", dir, err)
	}

	// Nothing anywhere: a clear error.
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveVaultDir(); err == nil {
		t.Error("no flag, no package, no config must be an error")
	}
}

func TestCheckinTargetsFromConfig(t *testing.T) {
	env := testenv.New(t)
	env.InitVault(t)
	cfg := env.Config(t)

	targets, err := checkinTargets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if targets.VaultDir != cfg.VaultDir || targets.DMSRepoDir == "" {
		t.Errorf("targets = %+v", targets)
	}
}

func TestPrintVerifyReportFlagsDrift(t *testing.T) {
	sc := deadswitch.DefaultSwitchConfig()
	report := &deadswitch.VerifyReport{
		DMSConfigured:           true,
		DMSRemote:               "git@github.com:owner/dms.git",
		WorkflowPresent:         true,
		WorkflowOutdated:        true,
		DeployedWorkflowVersion: 1,
	}
	out := capture(t, &os.Stdout, func() { printVerifyReport(report, sc) })
	if !strings.Contains(out, "OLDER AUTOMATION") {
		t.Errorf("outdated workflow must be flagged, got:\n%s", out)
	}
	if !strings.Contains(out, "switch seed") {
		t.Errorf("report must tell the user the fix (switch seed), got:\n%s", out)
	}
}

func TestPrintTriggeredWarningMatchesVaultKind(t *testing.T) {
	// V4 (sealed payload present): the DMS key alone is harmless.
	v4 := t.TempDir()
	if err := os.WriteFile(filepath.Join(v4, vault.SealedPayloadFile), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	out := capture(t, &os.Stdout, func() { printTriggeredWarning(v4) })
	if !strings.Contains(out, "DMS key") || strings.Contains(out, "kawarimi passwd") {
		t.Errorf("V4 warning wrong:\n%s", out)
	}

	// Legacy (no sealed payload): the passphrase itself may be out.
	out = capture(t, &os.Stdout, func() { printTriggeredWarning(t.TempDir()) })
	if !strings.Contains(out, "passphrase") || !strings.Contains(out, "kawarimi passwd") {
		t.Errorf("legacy warning wrong:\n%s", out)
	}
}

func TestPlural(t *testing.T) {
	if plural(1) != "y" || plural(2) != "ies" {
		t.Errorf("plural: entr%s / entr%s", plural(1), plural(2))
	}
}

func TestUnlockIdentitySlots(t *testing.T) {
	env := testenv.New(t)
	secrets := env.InitVault(t)
	cfg := env.Config(t)
	header, err := vault.LoadHeader(cfg.VaultDir)
	if err != nil {
		t.Fatal(err)
	}

	withStdin(t, env.Password()+"\n")
	id, err := unlockIdentity(cfg, header)
	if err != nil || id == "" {
		t.Fatalf("unlockIdentity via device key: %q err=%v", id, err)
	}

	if err := os.Remove(filepath.Join(env.AppDir, "device.key")); err != nil {
		t.Fatal(err)
	}
	withStdin(t, strings.Join(secrets.MnemonicWords, " ")+"\n")
	id2, err := unlockIdentity(cfg, header)
	if err != nil || id2 != id {
		t.Fatalf("unlockIdentity via mnemonic: %q err=%v (want %q)", id2, err, id)
	}
}

func TestUpdateHints(t *testing.T) {
	testenv.SetHome(t, t.TempDir())
	// Serve a newer release so the hint fires.
	name := selfupdate.AssetName("linux", "amd64")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "v9.9.9",
			"assets": []map[string]string{
				{"name": name, "browser_download_url": "http://unused"},
				{"name": selfupdate.AssetName("windows", "amd64"), "browser_download_url": "http://unused"},
				{"name": selfupdate.AssetName("darwin", "arm64"), "browser_download_url": "http://unused"},
				{"name": selfupdate.AssetName("darwin", "amd64"), "browser_download_url": "http://unused"},
				{"name": selfupdate.AssetName("linux", "arm64"), "browser_download_url": "http://unused"},
				{"name": "checksums.txt", "browser_download_url": "http://unused"},
				{"name": "checksums.txt.sig", "browser_download_url": "http://unused"},
			},
		})
	}))
	defer srv.Close()
	t.Setenv("KAWARIMI_GITHUB_API", srv.URL)

	oldVersion := version
	version = "0.1.0"
	t.Cleanup(func() { version = oldVersion })

	// refreshUpdateHint does the live check + caches + prints.
	out := capture(t, &os.Stderr, refreshUpdateHint)
	if !strings.Contains(out, "9.9.9") || !strings.Contains(out, "kawarimi update") {
		t.Errorf("refreshUpdateHint output:\n%s", out)
	}

	// printUpdateHintFromCache answers from the cache, no network.
	srv.Close()
	out = capture(t, &os.Stderr, printUpdateHintFromCache)
	if !strings.Contains(out, "9.9.9") {
		t.Errorf("printUpdateHintFromCache output:\n%s", out)
	}

	// A dev build never nags.
	version = "dev"
	out = capture(t, &os.Stderr, printUpdateHintFromCache)
	if out != "" {
		t.Errorf("dev build must print no hint, got:\n%s", out)
	}
}

func TestAutoLaunchGuardsOffPipes(t *testing.T) {
	// Under go test stdin is never a terminal, so both auto-launch contexts must
	// be off regardless of what is on disk — scripts and CI must never get a
	// surprise browser or wizard.
	withStdin(t, "")
	if recipientContext() {
		t.Error("recipientContext must be false on a non-tty stdin")
	}
	if ownerFirstRunContext() {
		t.Error("ownerFirstRunContext must be false on a non-tty stdin")
	}
}

func TestPackageHelperFunctions(t *testing.T) {
	if got := resolveSourceDir("/explicit"); got != "/explicit" {
		t.Errorf("resolveSourceDir explicit = %q", got)
	}

	dir := t.TempDir()
	for _, f := range []string{"kawarimi-linux-amd64", "kawarimi-windows-amd64.exe", "README.txt"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	bins := kawarimiBinariesIn(dir)
	if len(bins) != 2 {
		t.Errorf("kawarimiBinariesIn = %v, want the two kawarimi-* binaries", bins)
	}
}
