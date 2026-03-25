package components

import (
	"github.com/charmbracelet/lipgloss"
)

var statusStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#6B7280"))

var keyStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#7C3AED")).
	Bold(true)

// StatusBar renders contextual help hints at the bottom.
type StatusBar struct {
	hints []string
}

// NewStatusBar creates a status bar with key hints.
func NewStatusBar(hints ...string) StatusBar {
	return StatusBar{hints: hints}
}

// SetHints updates the displayed hints.
func (s *StatusBar) SetHints(hints ...string) {
	s.hints = hints
}

// View renders the status bar.
func (s StatusBar) View() string {
	if len(s.hints) == 0 {
		return ""
	}
	return statusStyle.Render(joinHints(s.hints))
}

func joinHints(hints []string) string {
	result := ""
	for i, h := range hints {
		if i > 0 {
			result += "  "
		}
		result += h
	}
	return result
}

// Hint creates a formatted "key: action" hint string.
func Hint(k, action string) string {
	return keyStyle.Render(k) + " " + statusStyle.Render(action)
}
