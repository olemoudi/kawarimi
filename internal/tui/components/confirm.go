package components

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	confirmBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#F59E0B")).
			Padding(1, 3).
			Width(50)

	confirmTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#F59E0B")).
			MarginBottom(1)
)

// ConfirmResult is the message sent when a confirmation dialog completes.
type ConfirmResult struct {
	Confirmed bool
	Tag       string // identifies which action was confirmed
}

// Confirm is a yes/no confirmation dialog.
type Confirm struct {
	Title    string
	Message  string
	Tag      string
	selected int // 0 = no, 1 = yes
	Active   bool
}

// NewConfirm creates a confirmation dialog.
func NewConfirm(title, message, tag string) Confirm {
	return Confirm{
		Title:   title,
		Message: message,
		Tag:     tag,
		Active:  true,
	}
}

// Update handles input for the confirmation dialog.
func (c Confirm) Update(msg tea.Msg) (Confirm, tea.Cmd) {
	if !c.Active {
		return c, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "left", "h":
			c.selected = 0
		case "right", "l":
			c.selected = 1
		case "y":
			c.Active = false
			return c, func() tea.Msg { return ConfirmResult{Confirmed: true, Tag: c.Tag} }
		case "n", "esc":
			c.Active = false
			return c, func() tea.Msg { return ConfirmResult{Confirmed: false, Tag: c.Tag} }
		case "enter":
			c.Active = false
			return c, func() tea.Msg { return ConfirmResult{Confirmed: c.selected == 1, Tag: c.Tag} }
		}
	}
	return c, nil
}

// View renders the confirmation dialog.
func (c Confirm) View() string {
	if !c.Active {
		return ""
	}

	noStyle := lipgloss.NewStyle().Padding(0, 2)
	yesStyle := lipgloss.NewStyle().Padding(0, 2)

	if c.selected == 0 {
		noStyle = noStyle.Bold(true).Foreground(lipgloss.Color("#EF4444"))
	}
	if c.selected == 1 {
		yesStyle = yesStyle.Bold(true).Foreground(lipgloss.Color("#10B981"))
	}

	buttons := fmt.Sprintf("  %s  %s",
		noStyle.Render("[ No ]"),
		yesStyle.Render("[ Yes ]"),
	)

	content := confirmTitle.Render(c.Title) + "\n" +
		c.Message + "\n\n" +
		buttons

	return confirmBox.Render(content)
}
