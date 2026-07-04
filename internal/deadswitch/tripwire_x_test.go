// External test package: these tests drive exported deadswitch APIs through the
// shared testenv harness (which imports deadswitch, so they cannot live in-package).
package deadswitch_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/olemoudi/kawarimi/internal/deadswitch"
	"github.com/olemoudi/kawarimi/internal/setup"
	"github.com/olemoudi/kawarimi/internal/testenv"
)

func TestSendTelegramWarningAndPing(t *testing.T) {
	tg := testenv.StartTelegram(t)
	cfg := deadswitch.DefaultSwitchConfig()
	cfg.TelegramBotToken = "tok"
	cfg.TelegramChatID = "42"

	if err := deadswitch.SendTelegramPing(cfg, 10); err != nil {
		t.Fatalf("SendTelegramPing: %v", err)
	}
	if err := deadswitch.SendTelegramWarning(cfg, 20); err != nil {
		t.Fatalf("SendTelegramWarning: %v", err)
	}

	pings := tg.Pings()
	if len(pings) != 2 {
		t.Fatalf("got %d messages, want 2", len(pings))
	}
	if !strings.Contains(pings[0], "Reminder") || strings.Contains(pings[0], "URGENT") {
		t.Errorf("ping text wrong: %s", pings[0])
	}
	if !strings.Contains(pings[1], "URGENT") || !strings.Contains(pings[1], "day 30") {
		t.Errorf("warning text must be urgent and name the release day: %s", pings[1])
	}
}

func TestResolveChatID(t *testing.T) {
	tg := testenv.StartTelegram(t)

	// No message yet: setup must tell the user to message the bot first.
	if _, err := deadswitch.ResolveChatID("tok"); err == nil {
		t.Error("ResolveChatID with no updates must error")
	}

	tg.ScriptAlive("7777")
	id, err := deadswitch.ResolveChatID("tok")
	if err != nil || id != "7777" {
		t.Fatalf("ResolveChatID = %q, %v; want 7777", id, err)
	}
}

// TestAlertIfRemoteStale proves the automatic tripwire: when the cloud heartbeat
// lags the local one, the owner gets exactly one email per day — and none while
// the switch is healthy.
func TestAlertIfRemoteStale(t *testing.T) {
	env := testenv.New(t)
	env.InitVault(t)
	mail := testenv.StartMail(t)
	sc := env.SwitchConfig(mail, "owner@test", "heir@test")
	env.ArmSwitch(t, sc, false)

	bare := testenv.BareRepo(t)
	cfg := env.Config(t)
	if _, err := setup.SeedSwitch(cfg, sc, bare, false); err != nil {
		t.Fatalf("SeedSwitch: %v", err)
	}
	targets := env.CheckinTargets(bare)

	// Healthy right after seeding: no alert.
	deadswitch.AlertIfRemoteStale(targets, sc, env.AppDir)
	if mail.Count() != 0 {
		t.Fatalf("healthy switch must not alert, got %d mails", mail.Count())
	}

	// Local heartbeat 3 days ahead of the remote (check-ins not landing): alert.
	env.SetLastCheckinDaysAgo(t, -3)
	deadswitch.AlertIfRemoteStale(targets, sc, env.AppDir)
	if mail.Count() != 1 {
		t.Fatalf("stale remote must alert the owner once, got %d mails", mail.Count())
	}
	last := mail.Last()
	if !mail.SentTo("owner@test") || !strings.Contains(last.Subject, "needs attention") {
		t.Errorf("alert mail wrong: to=%v subject=%q", last.To, last.Subject)
	}
	if !strings.Contains(last.Body, "stale") {
		t.Errorf("alert body must name the stale heartbeat: %s", last.Body)
	}

	// Same day again: deduplicated.
	deadswitch.AlertIfRemoteStale(targets, sc, env.AppDir)
	if mail.Count() != 1 {
		t.Errorf("repeat alert within a day must be suppressed, got %d mails", mail.Count())
	}

	// A day later (marker aged): it re-alerts.
	markerPath := filepath.Join(env.AppDir, "remote-alert-at")
	old := time.Now().UTC().Add(-21 * time.Hour).Format(time.RFC3339)
	if err := os.WriteFile(markerPath, []byte(old), 0600); err != nil {
		t.Fatal(err)
	}
	deadswitch.AlertIfRemoteStale(targets, sc, env.AppDir)
	if mail.Count() != 2 {
		t.Errorf("a day-old marker must allow a fresh alert, got %d mails", mail.Count())
	}

	// Healthy again (heartbeat pushed): marker cleared, no new alert.
	if _, err := deadswitch.RecordCheckin(targets, time.Now()); err != nil {
		t.Fatalf("RecordCheckin: %v", err)
	}
	deadswitch.AlertIfRemoteStale(targets, sc, env.AppDir)
	if mail.Count() != 2 {
		t.Errorf("healthy switch must stop alerting, got %d mails", mail.Count())
	}
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Error("recovery must clear the alert marker")
	}
}
