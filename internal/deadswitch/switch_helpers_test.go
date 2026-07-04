package deadswitch

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHasPingChannel(t *testing.T) {
	cfg := &SwitchConfig{PingChannels: []string{"email", "telegram"}}
	if !hasPingChannel(cfg, "email") || !hasPingChannel(cfg, "telegram") {
		t.Error("configured channels must be reported")
	}
	if hasPingChannel(cfg, "carrier-pigeon") {
		t.Error("unknown channel must be false")
	}
	// Legacy configs without the field fall back to email-only.
	legacy := &SwitchConfig{}
	if !hasPingChannel(legacy, "email") {
		t.Error("empty PingChannels must default to email")
	}
	if hasPingChannel(legacy, "telegram") {
		t.Error("empty PingChannels must not include telegram")
	}
}

func TestRecordFirstOverdueIsARatchet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "first-overdue")
	recordFirstOverdue(path)
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("first-overdue not stamped: %v", err)
	}
	// A later observation must not move the anchor (that would let a clock jump
	// forever postpone a local release).
	time.Sleep(1100 * time.Millisecond)
	recordFirstOverdue(path)
	second, _ := os.ReadFile(path)
	if string(first) != string(second) {
		t.Error("recordFirstOverdue must keep the original stamp")
	}
}

func TestOverdueLongEnough(t *testing.T) {
	cfg := DefaultSwitchConfig() // Warning1Days=14, FinalDays=30 → 16-day window
	dir := t.TempDir()

	// No stamp: never long enough (fail toward NOT releasing).
	if overdueLongEnough(filepath.Join(dir, "missing"), cfg) {
		t.Error("missing stamp must not justify a release")
	}

	// Garbage stamp: same.
	bad := filepath.Join(dir, "bad")
	os.WriteFile(bad, []byte("not a timestamp"), 0600)
	if overdueLongEnough(bad, cfg) {
		t.Error("unparseable stamp must not justify a release")
	}

	// Freshly overdue: not yet.
	fresh := filepath.Join(dir, "fresh")
	os.WriteFile(fresh, []byte(time.Now().UTC().Format(time.RFC3339)), 0600)
	if overdueLongEnough(fresh, cfg) {
		t.Error("a fresh overdue stamp must not justify a release")
	}

	// Overdue for longer than the warning ladder: yes.
	old := filepath.Join(dir, "old")
	os.WriteFile(old, []byte(time.Now().UTC().Add(-17*24*time.Hour).Format(time.RFC3339)), 0600)
	if !overdueLongEnough(old, cfg) {
		t.Error("17 days past first-overdue must exceed the 16-day ladder")
	}
}

// sendPing must dispatch to every configured channel and pick the urgent wording.
func TestSendPingTelegramDispatch(t *testing.T) {
	var texts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		texts = append(texts, r.FormValue("text"))
		w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer srv.Close()
	t.Setenv("KAWARIMI_TELEGRAM_API", srv.URL+"/bot")

	cfg := DefaultSwitchConfig()
	cfg.PingChannels = []string{"telegram"} // no email server needed
	cfg.TelegramBotToken = "tok"
	cfg.TelegramChatID = "42"

	if err := sendPing(cfg, 10, false); err != nil {
		t.Fatalf("sendPing reminder: %v", err)
	}
	if err := sendPing(cfg, 20, true); err != nil {
		t.Fatalf("sendPing urgent: %v", err)
	}
	if len(texts) != 2 {
		t.Fatalf("got %d telegram messages, want 2", len(texts))
	}
	if strings.Contains(texts[0], "URGENT") || !strings.Contains(texts[1], "URGENT") {
		t.Errorf("urgency wording wrong:\nreminder: %s\nurgent: %s", texts[0], texts[1])
	}
}
