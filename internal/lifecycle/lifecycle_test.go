package lifecycle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/olemoudi/kawarimi/internal/crypto"
	"github.com/olemoudi/kawarimi/internal/deadswitch"
	"github.com/olemoudi/kawarimi/internal/setup"
	"github.com/olemoudi/kawarimi/internal/testenv"
	"github.com/olemoudi/kawarimi/internal/vault"
)

func triggeredMarker(env *testenv.Env) string {
	return filepath.Join(env.AppDir, "switch-triggered")
}

// The full happy-sad path: owner sets up a local-release vault, goes silent past
// the final threshold, the switch fires, the recipient receives a working DMS key
// by email and opens the vault with their card passphrase — and a second evaluation
// does not re-fire.
func TestLifecycle_LocalReleaseThenRecipientDecrypts(t *testing.T) {
	env := testenv.New(t)
	mail := testenv.StartMail(t)
	secrets := env.InitVault(t)

	// Owner stores something worth recovering.
	v := env.OwnerVault(t)
	if _, err := v.AddNote("Bank", []byte("account 12345"), nil); err != nil {
		t.Fatalf("add note: %v", err)
	}

	sc := env.SwitchConfig(mail, "owner@test", "heir@test")
	env.ArmSwitch(t, sc, true) // local release: this machine may deliver the key
	env.SetLastCheckinDaysAgo(t, 40)
	env.SetFirstOverdueDaysAgo(t, 26) // overdue since ~day 14, as the daily timer would record

	if err := deadswitch.Evaluate(env.CheckinTargets(""), sc, env.AppDir); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	if !mail.SentTo("heir@test") {
		t.Fatal("recipient did not receive the release email")
	}
	if !strings.Contains(mail.Last().Body, secrets.DMSKeyB64) {
		t.Fatal("release email did not carry the DMS key")
	}

	// Recipient opens with the delivered key + the card passphrase.
	dmsKey, err := crypto.DecodeDMSKey(secrets.DMSKeyB64)
	if err != nil {
		t.Fatalf("decode DMS key: %v", err)
	}
	rv, err := vault.OpenSealedV4(env.VaultDir, dmsKey, secrets.RecipientPassphrase)
	if err != nil {
		t.Fatalf("recipient OpenSealedV4: %v", err)
	}
	if len(rv.Manifest.Entries) != 1 {
		t.Fatalf("recipient sees %d entries, want 1", len(rv.Manifest.Entries))
	}

	// Idempotent: it must not fire twice.
	before := mail.Count()
	if err := deadswitch.Evaluate(env.CheckinTargets(""), sc, env.AppDir); err != nil {
		t.Fatalf("second Evaluate: %v", err)
	}
	if mail.Count() != before {
		t.Errorf("switch re-fired: %d -> %d messages", before, mail.Count())
	}
}

// Warnings must alert the owner only, and must never leak the key to recipients.
func TestLifecycle_WarningsAlertOwnerNotRecipients(t *testing.T) {
	env := testenv.New(t)
	mail := testenv.StartMail(t)
	secrets := env.InitVault(t)
	sc := env.SwitchConfig(mail, "owner@test", "heir@test")
	env.ArmSwitch(t, sc, true)

	env.SetLastCheckinDaysAgo(t, 15) // Warning 1 window
	if err := deadswitch.Evaluate(env.CheckinTargets(""), sc, env.AppDir); err != nil {
		t.Fatalf("Evaluate (warning1): %v", err)
	}
	if !mail.SentTo("owner@test") {
		t.Fatal("owner not warned at warning 1")
	}
	if mail.SentTo("heir@test") {
		t.Fatal("recipient contacted during a warning stage")
	}
	if strings.Contains(mail.Last().Body, secrets.DMSKeyB64) {
		t.Fatal("warning email leaked the DMS key")
	}

	env.SetLastCheckinDaysAgo(t, 22) // Warning 2 window
	if err := deadswitch.Evaluate(env.CheckinTargets(""), sc, env.AppDir); err != nil {
		t.Fatalf("Evaluate (warning2): %v", err)
	}
	if !strings.Contains(strings.ToUpper(mail.Last().Subject), "URGENT") {
		t.Errorf("warning 2 subject not urgent: %q", mail.Last().Subject)
	}
}

// A missing or unparseable heartbeat must fail closed: never release, never mark
// triggered.
func TestLifecycle_FailClosedOnBadHeartbeat(t *testing.T) {
	env := testenv.New(t)
	mail := testenv.StartMail(t)
	env.InitVault(t)
	sc := env.SwitchConfig(mail, "owner@test", "heir@test")
	env.ArmSwitch(t, sc, true)

	env.CorruptLastCheckin(t)
	if err := deadswitch.Evaluate(env.CheckinTargets(""), sc, env.AppDir); err == nil {
		t.Fatal("expected an error evaluating an unparseable heartbeat")
	}
	if mail.SentTo("heir@test") {
		t.Fatal("released on an unparseable heartbeat — must fail closed")
	}
	if _, err := os.Stat(triggeredMarker(env)); err == nil {
		t.Fatal("marked triggered on an unparseable heartbeat")
	}

	// Missing entirely.
	if err := os.Remove(filepath.Join(env.VaultDir, vault.LastCheckinFile)); err != nil {
		t.Fatalf("remove heartbeat: %v", err)
	}
	if err := deadswitch.Evaluate(env.CheckinTargets(""), sc, env.AppDir); err == nil {
		t.Fatal("expected an error evaluating a missing heartbeat")
	}
	if mail.SentTo("heir@test") {
		t.Fatal("released on a missing heartbeat — must fail closed")
	}
}

// A forward clock jump must NOT trigger a local release on a single run: with no
// real-time overdue history, the switch alerts the owner instead of disclosing the
// key. Once genuine overdue time has accrued, it releases.
func TestLifecycle_ClockJumpDoesNotReleaseImmediately(t *testing.T) {
	env := testenv.New(t)
	mail := testenv.StartMail(t)
	env.InitVault(t)
	sc := env.SwitchConfig(mail, "owner@test", "heir@test")
	env.ArmSwitch(t, sc, true) // local release

	// Simulate a sudden clock jump: last check-in appears 400 days ago, but there is
	// no persisted overdue history (the switch never escalated over real time).
	env.SetLastCheckinDaysAgo(t, 400)
	if err := deadswitch.Evaluate(env.CheckinTargets(""), sc, env.AppDir); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if mail.SentTo("heir@test") {
		t.Fatal("released the key on a single overdue observation — clock-jump guard failed")
	}
	if !mail.SentTo("owner@test") {
		t.Fatal("owner not alerted when release was suppressed")
	}
	if _, err := os.Stat(triggeredMarker(env)); err == nil {
		t.Fatal("marked triggered despite the clock-jump guard")
	}

	// Now genuine overdue time has accrued (the ratchet anchor is old): it releases.
	env.SetFirstOverdueDaysAgo(t, 26)
	if err := deadswitch.Evaluate(env.CheckinTargets(""), sc, env.AppDir); err != nil {
		t.Fatalf("Evaluate (after real overdue time): %v", err)
	}
	if !mail.SentTo("heir@test") {
		t.Fatal("did not release once genuine overdue time had accrued")
	}
}

// A fresh check-in resets the timer: an overdue switch goes quiet again.
func TestLifecycle_CheckinResetsSwitch(t *testing.T) {
	env := testenv.New(t)
	mail := testenv.StartMail(t)
	env.InitVault(t)
	sc := env.SwitchConfig(mail, "owner@test", "heir@test")
	env.ArmSwitch(t, sc, true)

	env.SetLastCheckinDaysAgo(t, 40)
	if _, err := deadswitch.RecordCheckin(env.CheckinTargets(""), time.Now()); err != nil {
		t.Fatalf("RecordCheckin: %v", err)
	}
	if err := deadswitch.Evaluate(env.CheckinTargets(""), sc, env.AppDir); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if mail.Count() != 0 {
		t.Fatalf("expected no mail after a fresh check-in, got %d", mail.Count())
	}
}

// In cloud-only mode the owner's machine holds no key: at the final stage it alerts
// the owner and never delivers to recipients (the cloud does that).
func TestLifecycle_CloudOnlyFinalAlertsOwnerNoRelease(t *testing.T) {
	env := testenv.New(t)
	mail := testenv.StartMail(t)
	env.InitVault(t)
	sc := env.SwitchConfig(mail, "owner@test", "heir@test")
	env.ArmSwitch(t, sc, false) // cloud-only
	env.SetLastCheckinDaysAgo(t, 40)
	env.SetFirstOverdueDaysAgo(t, 26)

	if err := deadswitch.Evaluate(env.CheckinTargets(""), sc, env.AppDir); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !mail.SentTo("owner@test") {
		t.Fatal("owner not alerted at the final stage in cloud-only mode")
	}
	if mail.SentTo("heir@test") {
		t.Fatal("cloud-only machine delivered the key itself")
	}
	if _, err := os.Stat(triggeredMarker(env)); err != nil {
		t.Fatal("expected the triggered marker so the alert doesn't repeat daily")
	}
}

// Neither the DMS key alone nor the passphrase alone can open the vault.
func TestLifecycle_WrongSecretsCannotOpen(t *testing.T) {
	env := testenv.New(t)
	secrets := env.InitVault(t)
	dmsKey, err := crypto.DecodeDMSKey(secrets.DMSKeyB64)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := vault.OpenSealedV4(env.VaultDir, dmsKey, "totally the wrong six words here"); err == nil {
		t.Error("opened with the wrong passphrase")
	}
	bogus := make([]byte, len(dmsKey))
	if _, err := vault.OpenSealedV4(env.VaultDir, bogus, secrets.RecipientPassphrase); err == nil {
		t.Error("opened with the wrong DMS key")
	}
	if _, err := vault.OpenSealedV4(env.VaultDir, dmsKey, secrets.RecipientPassphrase); err != nil {
		t.Errorf("correct secrets failed to open: %v", err)
	}
}

// Rekeying invalidates the old DMS key while the physical card stays valid.
func TestLifecycle_RekeyInvalidatesOldKey(t *testing.T) {
	env := testenv.New(t)
	secrets := env.InitVault(t)
	oldKey, err := crypto.DecodeDMSKey(secrets.DMSKeyB64)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vault.OpenSealedV4(env.VaultDir, oldKey, secrets.RecipientPassphrase); err != nil {
		t.Fatalf("old key should open before rekey: %v", err)
	}

	entropy, err := crypto.DecodeMnemonic(secrets.MnemonicWords)
	if err != nil {
		t.Fatal(err)
	}
	newB64, err := setup.SealAndInstallV4(env.VaultDir, env.AppDir, entropy, secrets.RecipientPassphrase)
	crypto.ZeroBytes(entropy)
	if err != nil {
		t.Fatalf("rekey: %v", err)
	}
	if newB64 == secrets.DMSKeyB64 {
		t.Fatal("rekey produced the same DMS key")
	}

	if _, err := vault.OpenSealedV4(env.VaultDir, oldKey, secrets.RecipientPassphrase); err == nil {
		t.Error("old DMS key still opens after rekey")
	}
	newKey, err := crypto.DecodeDMSKey(newB64)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vault.OpenSealedV4(env.VaultDir, newKey, secrets.RecipientPassphrase); err != nil {
		t.Errorf("new DMS key (same card passphrase) failed to open: %v", err)
	}
}

// If an /alive reply refreshes the local heartbeat but the cloud push fails, the
// owner must be alerted — otherwise the local switch looks fine while the cloud
// heartbeat stays stale and could fire while the owner is alive (split brain).
func TestLifecycle_AliveCloudPushFailureAlertsOwner(t *testing.T) {
	env := testenv.New(t)
	mail := testenv.StartMail(t)
	tg := testenv.StartTelegram(t)
	env.InitVault(t)

	sc := env.SwitchConfig(mail, "owner@test", "heir@test")
	sc.TelegramBotToken = "test-token"
	sc.TelegramChatID = "12345"
	sc.PingChannels = []string{"email", "telegram"}
	env.ArmSwitch(t, sc, true)

	env.SetLastCheckinDaysAgo(t, 40)
	tg.ScriptAlive("12345")

	// Point the DMS remote at a non-existent repo so the cloud heartbeat push fails.
	targets := env.CheckinTargets(filepath.Join(t.TempDir(), "missing.git"))
	if err := deadswitch.Evaluate(targets, sc, env.AppDir); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if mail.SentTo("heir@test") {
		t.Fatal("released despite an /alive reply")
	}
	if !mail.SentTo("owner@test") {
		t.Fatal("owner not alerted that the /alive check-in failed to reach the cloud")
	}
}

// A Telegram /alive reply auto-checks-in during evaluation, keeping the switch quiet.
// An ALIVE email reply found over IMAP counts as a check-in: the switch must not
// warn or release, and the heartbeat must refresh — the mirror of the Telegram
// /alive path, over the second reply channel.
func TestLifecycle_IMAPAliveAutoCheckin(t *testing.T) {
	env := testenv.New(t)
	mail := testenv.StartMail(t)
	imap := testenv.StartIMAP(t)
	env.InitVault(t)

	sc := env.SwitchConfig(mail, "owner@test", "heir@test")
	sc.IMAPServer = imap.Host
	sc.IMAPPort = imap.Port
	env.ArmSwitch(t, sc, true)

	env.SetLastCheckinDaysAgo(t, 40)
	imap.ScriptAlive("3") // an ALIVE reply is sitting in the inbox

	if err := deadswitch.Evaluate(env.CheckinTargets(""), sc, env.AppDir); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if mail.SentTo("heir@test") {
		t.Fatal("released despite an ALIVE email reply")
	}
	days, err := deadswitch.DaysSinceCheckin(env.VaultDir)
	if err != nil {
		t.Fatal(err)
	}
	if days != 0 {
		t.Errorf("expected a fresh check-in after the ALIVE reply, got %d days", days)
	}
}

func TestLifecycle_TelegramAliveAutoCheckin(t *testing.T) {
	env := testenv.New(t)
	mail := testenv.StartMail(t)
	tg := testenv.StartTelegram(t)
	env.InitVault(t)

	sc := env.SwitchConfig(mail, "owner@test", "heir@test")
	sc.TelegramBotToken = "test-token"
	sc.TelegramChatID = "12345"
	sc.PingChannels = []string{"email", "telegram"}
	env.ArmSwitch(t, sc, true)

	env.SetLastCheckinDaysAgo(t, 40)
	tg.ScriptAlive("12345") // an /alive reply is waiting on the bot

	if err := deadswitch.Evaluate(env.CheckinTargets(""), sc, env.AppDir); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if mail.SentTo("heir@test") {
		t.Fatal("released despite an /alive reply")
	}
	days, err := deadswitch.DaysSinceCheckin(env.VaultDir)
	if err != nil {
		t.Fatal(err)
	}
	if days != 0 {
		t.Errorf("expected a fresh check-in after /alive, got %d days", days)
	}
}
