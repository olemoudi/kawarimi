package simenv

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TelegramServer mocks the Telegram Bot API. It captures sendMessage texts and can
// serve a scripted /alive reply for getUpdates. Callers route the production client
// here by setting KAWARIMI_TELEGRAM_API to URL() (the base must end in "bot",
// because the code builds telegramAPI()+token+"/sendMessage").
type TelegramServer struct {
	srv *httptest.Server

	mu     sync.Mutex
	pings  []string
	alive  bool
	chatID string
}

// StartTelegram starts the mock. It does not touch the environment.
func StartTelegram() (*TelegramServer, error) {
	ts := &TelegramServer{}
	ts.srv = httptest.NewServer(http.HandlerFunc(ts.handle))
	return ts, nil
}

// URL returns the base URL to set as KAWARIMI_TELEGRAM_API.
func (ts *TelegramServer) URL() string { return ts.srv.URL + "/bot" }

// Close stops the mock.
func (ts *TelegramServer) Close() { ts.srv.Close() }

// Pings returns the captured sendMessage texts.
func (ts *TelegramServer) Pings() []string {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return append([]string(nil), ts.pings...)
}

// ScriptAlive makes the next getUpdates return an "/alive" message from chatID.
func (ts *TelegramServer) ScriptAlive(chatID string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.alive = true
	ts.chatID = chatID
}

// ClearAlive removes the scripted /alive so it is consumed exactly once (the demo
// world calls this after the evaluator has acted on the reply).
func (ts *TelegramServer) ClearAlive() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.alive = false
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
