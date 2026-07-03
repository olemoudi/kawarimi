package deadswitch

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const telegramAPIBase = "https://api.telegram.org/bot"

// telegramAPIBaseOverride allows tests to redirect API calls to a test server.
var telegramAPIBaseOverride string

func telegramAPI() string {
	if telegramAPIBaseOverride != "" {
		return telegramAPIBaseOverride
	}
	return telegramAPIBase
}

// telegramResponse is the generic Telegram API response wrapper.
type telegramResponse struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result"`
	Desc   string          `json:"description,omitempty"`
}

// telegramUpdate represents a Telegram update (message, etc).
type telegramUpdate struct {
	UpdateID int              `json:"update_id"`
	Message  *telegramMessage `json:"message,omitempty"`
}

type telegramMessage struct {
	MessageID int           `json:"message_id"`
	From      *telegramUser `json:"from,omitempty"`
	Chat      telegramChat  `json:"chat"`
	Date      int64         `json:"date"`
	Text      string        `json:"text"`
}

type telegramUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username,omitempty"`
}

type telegramChat struct {
	ID int64 `json:"id"`
}

// SendTelegramMessage sends a text message to a Telegram chat.
func SendTelegramMessage(token, chatID, text string) error {
	apiURL := telegramAPI() + token + "/sendMessage"

	resp, err := http.PostForm(apiURL, url.Values{
		"chat_id":    {chatID},
		"text":       {text},
		"parse_mode": {"Markdown"},
	})
	if err != nil {
		return fmt.Errorf("telegram sendMessage: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result telegramResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("telegram response parse: %w", err)
	}
	if !result.OK {
		return fmt.Errorf("telegram API error: %s", result.Desc)
	}
	return nil
}

// CheckForAlive checks recent Telegram messages for an /alive command. Returns true
// if an /alive message from the expected chat was received since `since`. It tracks
// the last processed update_id (in appDir/telegram-offset) and passes it as the
// getUpdates offset, so a busy bot cannot bury an /alive beyond the fetch window.
func CheckForAlive(token, chatID string, since time.Time, appDir string) (bool, error) {
	apiURL := telegramAPI() + token + "/getUpdates"

	offset := readTelegramOffset(appDir)
	vals := url.Values{"timeout": {"0"}, "limit": {"100"}}
	if offset > 0 {
		vals.Set("offset", strconv.Itoa(offset))
	}

	resp, err := http.PostForm(apiURL, vals)
	if err != nil {
		return false, fmt.Errorf("telegram getUpdates: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result telegramResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return false, fmt.Errorf("telegram response parse: %w", err)
	}
	if !result.OK {
		return false, fmt.Errorf("telegram API error: %s", result.Desc)
	}

	var updates []telegramUpdate
	if err := json.Unmarshal(result.Result, &updates); err != nil {
		return false, fmt.Errorf("parsing updates: %w", err)
	}

	expectedChatID, _ := strconv.ParseInt(chatID, 10, 64)

	alive := false
	maxID := offset - 1
	for _, u := range updates {
		if u.UpdateID > maxID {
			maxID = u.UpdateID
		}
		if u.Message == nil {
			continue
		}
		msgTime := time.Unix(u.Message.Date, 0)
		if msgTime.Before(since) {
			continue
		}
		if u.Message.Chat.ID != expectedChatID {
			continue
		}
		text := strings.TrimSpace(strings.ToLower(u.Message.Text))
		if text == "/alive" || text == "alive" {
			alive = true
		}
	}

	// Advance the offset past everything we just saw so the next call fetches only
	// newer updates.
	if len(updates) > 0 {
		writeTelegramOffset(appDir, maxID+1)
	}
	return alive, nil
}

// readTelegramOffset returns the stored getUpdates offset, or 0 if none.
func readTelegramOffset(appDir string) int {
	data, err := os.ReadFile(filepath.Join(appDir, "telegram-offset"))
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return n
}

func writeTelegramOffset(appDir string, offset int) {
	_ = os.WriteFile(filepath.Join(appDir, "telegram-offset"), []byte(strconv.Itoa(offset)), 0600)
}

// ResolveChatID sends a test message and waits for the user to message the bot,
// then returns the chat ID. This helps during setup.
func ResolveChatID(token string) (string, error) {
	// Get recent updates to find the chat ID
	apiURL := telegramAPI() + token + "/getUpdates"

	resp, err := http.PostForm(apiURL, url.Values{
		"timeout": {"0"},
		"limit":   {"10"},
	})
	if err != nil {
		return "", fmt.Errorf("telegram getUpdates: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result telegramResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("telegram response parse: %w", err)
	}
	if !result.OK {
		return "", fmt.Errorf("telegram API error: %s", result.Desc)
	}

	var updates []telegramUpdate
	if err := json.Unmarshal(result.Result, &updates); err != nil {
		return "", fmt.Errorf("parsing updates: %w", err)
	}

	// Return the most recent chat ID
	for i := len(updates) - 1; i >= 0; i-- {
		if updates[i].Message != nil {
			return strconv.FormatInt(updates[i].Message.Chat.ID, 10), nil
		}
	}

	return "", fmt.Errorf("no messages found — send any message to the bot first")
}

// SendTelegramPing sends a check-in reminder via Telegram.
func SendTelegramPing(cfg *SwitchConfig, daysSince int) error {
	text := fmt.Sprintf(
		"*Kawarimi Check-in Reminder*\n\n"+
			"You haven't checked in for *%d days*.\n\n"+
			"Reply /alive to confirm you're OK.",
		daysSince,
	)
	return SendTelegramMessage(cfg.TelegramBotToken, cfg.TelegramChatID, text)
}

// SendTelegramWarning sends an urgent warning via Telegram.
func SendTelegramWarning(cfg *SwitchConfig, daysSince int) error {
	text := fmt.Sprintf(
		"*URGENT: Kawarimi Check-in Overdue*\n\n"+
			"You haven't checked in for *%d days*.\n\n"+
			"Reply /alive NOW to prevent vault release on day %d.",
		daysSince, cfg.FinalDays,
	)
	return SendTelegramMessage(cfg.TelegramBotToken, cfg.TelegramChatID, text)
}
