package testenv

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// TelegramServer mocks the Telegram Bot API. It captures sendMessage texts and can
// serve a scripted /alive reply for getUpdates. It sets KAWARIMI_TELEGRAM_API so the
// deadswitch package's telegramAPI() routes here.
type TelegramServer struct {
	srv *httptest.Server

	mu     sync.Mutex
	pings  []string
	alive  bool
	chatID string
}

// StartTelegram starts the mock and points the deadswitch client at it.
func StartTelegram(t testing.TB) *TelegramServer {
	t.Helper()
	ts := &TelegramServer{}
	ts.srv = httptest.NewServer(http.HandlerFunc(ts.handle))
	// The code builds telegramAPI()+token+"/sendMessage", so the base must end in "bot".
	t.Setenv("KAWARIMI_TELEGRAM_API", ts.srv.URL+"/bot")
	t.Cleanup(ts.srv.Close)
	return ts
}

// Pings returns the captured sendMessage texts.
func (ts *TelegramServer) Pings() []string {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return append([]string(nil), ts.pings...)
}

// PingCount returns how many messages the bot has sent.
func (ts *TelegramServer) PingCount() int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return len(ts.pings)
}

// ScriptAlive makes the next getUpdates return an "/alive" message from chatID.
func (ts *TelegramServer) ScriptAlive(chatID string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.alive = true
	ts.chatID = chatID
}

func (ts *TelegramServer) handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch {
	case strings.HasSuffix(r.URL.Path, "/sendMessage"):
		_ = r.ParseForm()
		ts.mu.Lock()
		ts.pings = append(ts.pings, r.FormValue("text"))
		ts.mu.Unlock()
		w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))

	case strings.HasSuffix(r.URL.Path, "/getUpdates"):
		ts.mu.Lock()
		alive, chatID := ts.alive, ts.chatID
		ts.mu.Unlock()
		result := []map[string]any{}
		if alive {
			id, _ := strconv.ParseInt(chatID, 10, 64)
			result = append(result, map[string]any{
				"update_id": 1,
				"message": map[string]any{
					"message_id": 1,
					"chat":       map[string]any{"id": id},
					"date":       time.Now().Unix(),
					"text":       "/alive",
				},
			})
		}
		resp, _ := json.Marshal(map[string]any{"ok": true, "result": result})
		w.Write(resp)

	default:
		w.Write([]byte(`{"ok":true,"result":{}}`))
	}
}
