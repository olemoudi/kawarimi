package demo_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olemoudi/kawarimi/internal/demo"
	"github.com/olemoudi/kawarimi/internal/testenv"
	"github.com/olemoudi/kawarimi/internal/vault"
)

// newTestWorld isolates the process env FIRST (t.Setenv restores on cleanup),
// because NewWorld deliberately repoints HOME and the mock API env vars.
func newTestWorld(t *testing.T, opts demo.Options) *demo.World {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("KAWARIMI_TELEGRAM_API", "")
	t.Setenv("KAWARIMI_GITHUB_API", "")
	w, err := demo.NewWorld(opts)
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	t.Cleanup(func() { w.Close() })
	return w
}

// The seeded world must be a REAL armed installation: encrypted vault with
// entries, workflow + heartbeat on the cloud repo, a recognizable package zip,
// and the V4 property that the captured DMS key + the card open the vault.
func TestNewWorldSeedsSandbox(t *testing.T) {
	w := newTestWorld(t, demo.Options{ForceLocalEngine: true})

	snap, err := w.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snap.Day != 0 || snap.Stage != "normal" || snap.Released {
		t.Errorf("fresh world: day=%d stage=%s released=%v", snap.Day, snap.Stage, snap.Released)
	}
	if snap.Owner.EntryCount != 3 {
		t.Errorf("entry count = %d, want 3", snap.Owner.EntryCount)
	}
	if snap.CardWords == "" {
		t.Error("the card words must be revealed to the demo user")
	}
	if snap.KeyB64 != "" {
		t.Error("the DMS key must NOT be revealed before release")
	}
	if !snap.Cloud.WorkflowPresent || snap.Cloud.Heartbeat == "" {
		t.Errorf("cloud repo not seeded: %+v", snap.Cloud)
	}

	home := os.Getenv("HOME")
	pkg := vault.FindPackageZip(home)
	if pkg == "" {
		t.Fatal("the sandbox package zip must be a recognizable kawarimi package")
	}

	// The package really is a sealed V4 package (the recipient beats of the story
	// tests prove the key + card open it end to end).
	extract := t.TempDir()
	vaultDir, err := vault.ExtractPackage(pkg, extract)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(vaultDir, vault.SealedPayloadFile)); err != nil {
		t.Fatalf("package missing sealed payload: %v", err)
	}
}

// The whole story through the LOCAL engine (runs on every OS): quiet → warning
// (mail + Telegram ping) → check-in resets → silence to release → exactly one
// release mail that leaks neither the card nor the mnemonic → post-release
// silence → recipient negatives then success.
func TestWorldLocalEngineStory(t *testing.T) {
	w := newTestWorld(t, demo.Options{ForceLocalEngine: true})

	// Day 2: everyone is quiet.
	snap, err := w.Advance(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.OwnerInbox) != 0 || len(snap.RecipientInbox) != 0 {
		t.Fatalf("day 2 must be quiet, got owner=%d recipient=%d mails",
			len(snap.OwnerInbox), len(snap.RecipientInbox))
	}

	// Day 15: the owner is warned by mail AND on the phone; the heir hears nothing.
	if snap, err = w.Advance(13); err != nil {
		t.Fatal(err)
	}
	if snap.Stage != "warning1" || len(snap.OwnerInbox) == 0 {
		t.Fatalf("day 15: stage=%s ownerInbox=%d", snap.Stage, len(snap.OwnerInbox))
	}
	if len(snap.Phone) == 0 {
		t.Error("day 15: the owner's phone must show a Telegram reminder")
	}
	if len(snap.RecipientInbox) != 0 {
		t.Fatal("recipients must hear NOTHING during warnings")
	}

	// The owner checks in: back to day 0.
	if snap, err = w.Checkin(); err != nil {
		t.Fatal(err)
	}
	if snap.Day != 0 || snap.Stage != "normal" {
		t.Fatalf("after checkin: day=%d stage=%s", snap.Day, snap.Stage)
	}

	// Then silence to the end: release fires at FinalDays.
	if snap, err = w.Advance(32); err != nil {
		t.Fatal(err)
	}
	if !snap.Released {
		t.Fatal("day 32 of silence: the switch must have released")
	}
	var releases []string
	for _, m := range snap.RecipientInbox {
		if m.Release {
			releases = append(releases, m.Body)
		}
	}
	if len(releases) != 1 {
		t.Fatalf("want exactly one release mail (local engine releases once), got %d", len(releases))
	}
	body := releases[0]
	if !strings.Contains(body, snap.KeyB64) {
		t.Error("release mail must carry the DMS key")
	}
	if snap.KeyB64 == "" {
		t.Error("the key must be revealed in the snapshot once released")
	}
	if strings.Contains(body, snap.CardWords) {
		t.Fatal("release mail must NEVER contain the card words")
	}

	// Post-release days stay silent (switch-triggered idempotence).
	before := len(snap.RecipientInbox)
	if snap, err = w.Advance(2); err != nil {
		t.Fatal(err)
	}
	if len(snap.RecipientInbox) != before {
		t.Fatal("the local engine must not re-mail recipients after releasing")
	}

	// Recipient beats: attacker negatives, then the heir succeeds.
	if _, err := w.RecipientOpen("not-a-key", snap.CardWords); err == nil {
		t.Fatal("garbage key must not open the vault")
	}
	if _, err := w.RecipientOpen(snap.KeyB64, "wrong words entirely six here"); err == nil {
		t.Fatal("wrong card words must not open the vault")
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), "recipient-machine", "decrypted")); !os.IsNotExist(err) {
		t.Fatal("failed attempts must leave no decrypted output")
	}
	snap, err = w.RecipientOpen(snap.KeyB64, snap.CardWords)
	if err != nil {
		t.Fatalf("the heir with key + card must open the vault: %v", err)
	}
	if !snap.Recipient.Opened || !strings.Contains(snap.Recipient.Index, "Bank instructions") {
		t.Fatalf("recipient panel after open: %+v", snap.Recipient)
	}
	foundIndex := false
	for _, f := range snap.Recipient.Files {
		if f == "INDEX.md" {
			foundIndex = true
		}
	}
	if !foundIndex {
		t.Errorf("decrypted files must include INDEX.md: %v", snap.Recipient.Files)
	}
}

// A scripted /alive from the phone must auto-check-in on the next daily tick,
// through the real CheckForAlive path — and only once.
func TestWorldTelegramAliveAutoCheckin(t *testing.T) {
	w := newTestWorld(t, demo.Options{ForceLocalEngine: true})

	if _, err := w.Advance(5); err != nil {
		t.Fatal(err)
	}
	if _, err := w.TelegramAlive(); err != nil {
		t.Fatal(err)
	}
	snap, err := w.Advance(1)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Day != 0 {
		t.Fatalf("the /alive reply must auto-check-in, day = %d", snap.Day)
	}
	// Consumed once: the next advance is a normal silent day.
	if snap, err = w.Advance(1); err != nil {
		t.Fatal(err)
	}
	if snap.Day != 1 {
		t.Fatalf("the /alive script must be consumed exactly once, day = %d", snap.Day)
	}
}

// The whole story through the CLOUD engine: the REAL generated deadman.yml
// executed under bash, day by day.
func TestWorldCloudStory(t *testing.T) {
	testenv.RequireWorkflowRunner(t)
	w := newTestWorld(t, demo.Options{})

	snap, err := w.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snap.Engine != "cloud" {
		t.Fatalf("on a workflow-capable machine the engine must be cloud, got %s", snap.Engine)
	}

	if snap, err = w.Advance(2); err != nil {
		t.Fatal(err)
	}
	if len(snap.RecipientInbox) != 0 {
		t.Fatal("day 2 must be quiet")
	}
	if len(snap.Cloud.Cron) == 0 || snap.Cloud.Cron[0].Status != "ok" {
		t.Fatalf("cloud cron log missing/wrong: %+v", snap.Cloud.Cron)
	}

	if snap, err = w.Checkin(); err != nil {
		t.Fatal(err)
	}
	if snap, err = w.Advance(31); err != nil {
		t.Fatal(err)
	}
	if !snap.Released {
		t.Fatal("day 31 of silence: the cloud workflow must have released")
	}
	release := snap.RecipientInbox[len(snap.RecipientInbox)-1]
	if release.Via != "cloud" {
		t.Errorf("the release must come from the cloud engine, got via=%s", release.Via)
	}
	if !strings.Contains(release.Body, snap.KeyB64) {
		t.Error("cloud release mail must carry the DMS key")
	}
	if strings.Contains(release.Body, snap.CardWords) {
		t.Fatal("cloud release mail must NEVER contain the card words")
	}

	// The heir opens the vault with what the email + card gave them.
	if snap, err = w.RecipientOpen(snap.KeyB64, snap.CardWords); err != nil {
		t.Fatalf("heir open: %v", err)
	}
	if !snap.Recipient.Opened {
		t.Fatal("recipient panel must show the vault as opened")
	}
}

// Reset must produce a brand-new world: fresh secrets, clean inboxes, day 0.
func TestWorldReset(t *testing.T) {
	w := newTestWorld(t, demo.Options{ForceLocalEngine: true})

	before, err := w.Advance(15)
	if err != nil {
		t.Fatal(err)
	}
	if len(before.OwnerInbox) == 0 {
		t.Fatal("expected warnings before the reset")
	}

	after, err := w.Reset()
	if err != nil {
		t.Fatal(err)
	}
	if after.Day != 0 || len(after.OwnerInbox) != 0 || after.Released {
		t.Errorf("reset world not fresh: %+v", after)
	}
	if after.CardWords == before.CardWords {
		t.Error("reset must mint fresh secrets (card words unchanged)")
	}
}
