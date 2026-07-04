package testenv

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/olemoudi/kawarimi/internal/config"
	"github.com/olemoudi/kawarimi/internal/crypto"
	"github.com/olemoudi/kawarimi/internal/deadswitch"
	"github.com/olemoudi/kawarimi/internal/setup"
	"github.com/olemoudi/kawarimi/internal/vault"
)

// Env is an isolated kawarimi installation: a temp HOME so config, the app dir, and
// the vault all live in a throwaway tree with no effect on the real machine.
type Env struct {
	Home     string
	AppDir   string
	VaultDir string
	Secrets  *setup.InitSecrets // set by InitVault
}

// New sets up an isolated HOME for the duration of the test.
func New(t testing.TB) *Env {
	t.Helper()
	home := SetHome(t, t.TempDir())
	return &Env{
		Home:     home,
		AppDir:   filepath.Join(home, config.AppDir),
		VaultDir: filepath.Join(home, "vault"),
	}
}

// SetHome points the process home at dir for the duration of the test. Both HOME
// and USERPROFILE are set: os.UserHomeDir (which config uses) reads USERPROFILE on
// Windows, so setting HOME alone silently loses isolation there.
func SetHome(t testing.TB, dir string) string {
	t.Helper()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	return dir
}

// Password is the owner password used for the isolated vault.
func (e *Env) Password() string { return "lifecycle-test-password" }

// InitVault creates a V4 vault with fast KDF params and records its secrets on e.
func (e *Env) InitVault(t testing.TB) *setup.InitSecrets {
	t.Helper()
	fast := crypto.TestParams()
	s, err := setup.InitVault(setup.InitOptions{
		VaultDir:          e.VaultDir,
		Password:          e.Password(),
		MnemonicKDFParams: &fast,
		OwnerKDFParams:    &fast,
	})
	if err != nil {
		t.Fatalf("InitVault: %v", err)
	}
	e.Secrets = s
	return s
}

// OwnerVault opens the vault the owner way (via the mnemonic slot, using the words
// InitVault produced) so a test can add entries. Fails the test on error.
func (e *Env) OwnerVault(t testing.TB) *vault.Vault {
	t.Helper()
	if e.Secrets == nil {
		t.Fatal("OwnerVault requires InitVault first")
	}
	h, err := vault.LoadHeader(e.VaultDir)
	if err != nil {
		t.Fatalf("load header: %v", err)
	}
	_, identity, err := h.OpenWithMnemonic(e.Secrets.MnemonicWords)
	if err != nil {
		t.Fatalf("open with mnemonic: %v", err)
	}
	v, err := vault.OpenV2(e.VaultDir, identity, h.AgeRecipient)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	return v
}

// SwitchConfig returns a SwitchConfig wired to the mock mail server, with the given
// owner + recipient emails and default thresholds (14/21/30).
func (e *Env) SwitchConfig(mail *MailServer, userEmail string, recipients ...string) *deadswitch.SwitchConfig {
	sc := deadswitch.DefaultSwitchConfig()
	sc.SMTPServer = mail.Host
	sc.SMTPPort = mail.Port
	sc.SMTPUsername = "bot@testenv"
	sc.SMTPPassword = "smtp-pw"
	sc.SenderEmail = "bot@testenv"
	sc.UserEmail = userEmail
	sc.Recipients = recipients
	sc.PingChannels = []string{"email"}
	sc.VaultPackageLocation = "https://example.test/vault.zip"
	return sc
}

// ArmSwitch stores the switch payload (cloud-only or local-release) and saves the
// switch config locally — the same steps the CLI/GUI perform, minus the cloud seed.
func (e *Env) ArmSwitch(t testing.TB, sc *deadswitch.SwitchConfig, localRelease bool) {
	t.Helper()
	if err := setup.StoreSwitchPayloadForMode(e.AppDir, localRelease); err != nil {
		t.Fatalf("store switch payload: %v", err)
	}
	if err := deadswitch.SaveSwitchConfig(e.AppDir, sc); err != nil {
		t.Fatalf("save switch config: %v", err)
	}
}

// Config loads the on-disk config for this env.
func (e *Env) Config(t testing.TB) *config.Config {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg
}

// CheckinTargets builds check-in targets; pass a bare repo path as dmsRemote to push
// the heartbeat to the mock cloud repo, or "" for local-only.
func (e *Env) CheckinTargets(dmsRemote string) deadswitch.CheckinTargets {
	return deadswitch.CheckinTargets{
		VaultDir:   e.VaultDir,
		DMSRepoDir: filepath.Join(e.AppDir, config.DMSRepoName),
		DMSRemote:  dmsRemote,
	}
}

// SetLastCheckinDaysAgo writes the local heartbeat to now-days days ago.
func (e *Env) SetLastCheckinDaysAgo(t testing.TB, days int) {
	t.Helper()
	ts := time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339)
	e.writeCheckin(t, ts+"\n")
}

// SetFirstOverdueDaysAgo seeds the clock-jump ratchet anchor to now-days, so a
// scenario can represent a switch that has genuinely been overdue for that long (as
// the daily timer would have recorded) and is therefore eligible for local release.
func (e *Env) SetFirstOverdueDaysAgo(t testing.TB, days int) {
	t.Helper()
	if err := os.MkdirAll(e.AppDir, 0700); err != nil {
		t.Fatalf("mkdir app dir: %v", err)
	}
	ts := time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339)
	if err := os.WriteFile(filepath.Join(e.AppDir, "first-overdue-at"), []byte(ts+"\n"), 0600); err != nil {
		t.Fatalf("write first-overdue-at: %v", err)
	}
}

// CorruptLastCheckin writes an unparseable heartbeat (fail-closed scenario).
func (e *Env) CorruptLastCheckin(t testing.TB) {
	e.writeCheckin(t, "not-a-timestamp\n")
}

func (e *Env) writeCheckin(t testing.TB, content string) {
	t.Helper()
	p := filepath.Join(e.VaultDir, vault.LastCheckinFile)
	if err := os.WriteFile(p, []byte(content), 0600); err != nil {
		t.Fatalf("write last_checkin: %v", err)
	}
}
