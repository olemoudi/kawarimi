package components

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ToastLevel indicates the severity of the toast message.
type ToastLevel int

const (
	ToastInfo ToastLevel = iota
	ToastSuccess
	ToastError
)

type toastTimeout struct{}

// Toast displays a transient message that auto-dismisses.
type Toast struct {
	Message  string
	Level    ToastLevel
	visible  bool
	duration time.Duration
}

// NewToast creates a new toast manager.
func NewToast() Toast {
	return Toast{duration: 3 * time.Second}
}

// Show displays a toast message.
func (t *Toast) Show(msg string, level ToastLevel) tea.Cmd {
	t.Message = msg
	t.Level = level
	t.visible = true
	d := t.duration
	return tea.Tick(d, func(time.Time) tea.Msg {
		return toastTimeout{}
	})
}

// Update handles toast timeout.
func (t Toast) Update(msg tea.Msg) (Toast, tea.Cmd) {
	if _, ok := msg.(toastTimeout); ok {
		t.visible = false
	}
	return t, nil
}

// View renders the toast if visible.
func (t Toast) View() string {
	if !t.visible {
		return ""
	}

	var style lipgloss.Style
	switch t.Level {
	case ToastSuccess:
		style = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#10B981")).
			Bold(true)
	case ToastError:
		style = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#EF4444")).
			Bold(true)
	default:
		style = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#06B6D4"))
	}

	return style.Render(t.Message)
}
