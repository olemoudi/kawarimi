package testenv

import (
	"testing"

	"github.com/olemoudi/kawarimi/internal/simenv"
)

// TelegramServer mocks the Telegram Bot API.
type TelegramServer = simenv.TelegramServer

// StartTelegram starts the mock and points the deadswitch client at it.
func StartTelegram(t testing.TB) *TelegramServer {
	t.Helper()
	ts, err := simenv.StartTelegram()
	if err != nil {
		t.Fatalf("start telegram mock: %v", err)
	}
	t.Setenv("KAWARIMI_TELEGRAM_API", ts.URL())
	t.Cleanup(ts.Close)
	return ts
}
