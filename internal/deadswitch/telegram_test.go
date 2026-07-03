package deadswitch

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSendTelegramMessage(t *testing.T) {
	var receivedChatID, receivedText string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/sendMessage") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		r.ParseForm()
		receivedChatID = r.FormValue("chat_id")
		receivedText = r.FormValue("text")
		json.NewEncoder(w).Encode(telegramResponse{OK: true})
	}))
	defer server.Close()

	// Override the API base for testing
	oldBase := telegramAPIBase
	defer func() { telegramAPIBaseOverride = "" }()
	telegramAPIBaseOverride = server.URL + "/bot"

	err := SendTelegramMessage("test-token", "12345", "hello")
	if err != nil {
		t.Fatalf("SendTelegramMessage: %v", err)
	}
	if receivedChatID != "12345" {
		t.Errorf("chat_id: got %q, want 12345", receivedChatID)
	}
	if receivedText != "hello" {
		t.Errorf("text: got %q, want hello", receivedText)
	}

	_ = oldBase
}

func TestCheckForAlive(t *testing.T) {
	now := time.Now()
	updates := []telegramUpdate{
		{
			UpdateID: 1,
			Message: &telegramMessage{
				Chat: telegramChat{ID: 12345},
				Date: now.Add(-1 * time.Hour).Unix(),
				Text: "hello",
			},
		},
		{
			UpdateID: 2,
			Message: &telegramMessage{
				Chat: telegramChat{ID: 12345},
				Date: now.Unix(),
				Text: "/alive",
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result, _ := json.Marshal(updates)
		json.NewEncoder(w).Encode(telegramResponse{OK: true, Result: result})
	}))
	defer server.Close()
	defer func() { telegramAPIBaseOverride = "" }()
	telegramAPIBaseOverride = server.URL + "/bot"

	// Should find /alive
	found, err := CheckForAlive("token", "12345", now.Add(-2*time.Hour), t.TempDir())
	if err != nil {
		t.Fatalf("CheckForAlive: %v", err)
	}
	if !found {
		t.Error("expected to find /alive")
	}

	// Should not find /alive from wrong chat
	found, err = CheckForAlive("token", "99999", now.Add(-2*time.Hour), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Error("should not find /alive from wrong chat")
	}

	// Should not find /alive before the since time
	found, err = CheckForAlive("token", "12345", now.Add(1*time.Hour), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Error("should not find /alive after since time")
	}
}

// TestCheckForAliveTracksOffset verifies that the last processed update_id is
// persisted and sent as the getUpdates offset on the next call.
func TestCheckForAliveTracksOffset(t *testing.T) {
	now := time.Now()
	updates := []telegramUpdate{
		{UpdateID: 42, Message: &telegramMessage{Date: now.Unix(), Text: "/alive", Chat: telegramChat{ID: 12345}}},
	}

	var gotOffset string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		gotOffset = r.FormValue("offset")
		result, _ := json.Marshal(updates)
		json.NewEncoder(w).Encode(telegramResponse{OK: true, Result: result})
	}))
	defer server.Close()
	defer func() { telegramAPIBaseOverride = "" }()
	telegramAPIBaseOverride = server.URL + "/bot"

	appDir := t.TempDir()
	if _, err := CheckForAlive("token", "12345", now.Add(-2*time.Hour), appDir); err != nil {
		t.Fatalf("first CheckForAlive: %v", err)
	}
	// Second call should send offset = maxID+1 = 43.
	if _, err := CheckForAlive("token", "12345", now.Add(-2*time.Hour), appDir); err != nil {
		t.Fatalf("second CheckForAlive: %v", err)
	}
	if gotOffset != "43" {
		t.Errorf("expected offset=43 on the second call, got %q", gotOffset)
	}
}

func TestSendTelegramPing(t *testing.T) {
	var receivedText string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		receivedText = r.FormValue("text")
		json.NewEncoder(w).Encode(telegramResponse{OK: true})
	}))
	defer server.Close()
	defer func() { telegramAPIBaseOverride = "" }()
	telegramAPIBaseOverride = server.URL + "/bot"

	cfg := &SwitchConfig{
		TelegramBotToken: "token",
		TelegramChatID:   "12345",
		FinalDays:        30,
	}

	if err := SendTelegramPing(cfg, 14); err != nil {
		t.Fatalf("SendTelegramPing: %v", err)
	}
	if !strings.Contains(receivedText, "14 days") {
		t.Errorf("ping should mention days: %s", receivedText)
	}
}
