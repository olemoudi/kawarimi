package screens

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/olemoudi/kawarimi/internal/tui/components"
	"github.com/olemoudi/kawarimi/internal/vault"
)

// BackToListMsg returns to the entry list.
type BackToListMsg struct{}

// EditEntryMsg requests editing the current entry.
type EditEntryMsg struct{ Entry *vault.Entry }

// Detail shows decrypted content of a single entry.
type Detail struct {
	entry     *vault.Entry
	content   string
	viewport  viewport.Model
	statusBar components.StatusBar
	ready     bool
	width     int
	height    int
}

// NewDetail creates an entry detail viewer.
func NewDetail(entry *vault.Entry, v *vault.Vault, width, height int) Detail {
	data, err := v.ShowEntry(entry)
	var content string
	if err != nil {
		content = fmt.Sprintf("Error decrypting: %v", err)
	} else {
		content = formatEntryContent(entry, data)
	}

	sb := components.NewStatusBar(
		components.Hint("esc", "back"),
		components.Hint("e", "edit"),
	)

	vp := viewport.New(width, height-8)
	vp.SetContent(content)

	return Detail{
		entry:     entry,
		content:   content,
		viewport:  vp,
		statusBar: sb,
		ready:     true,
		width:     width,
		height:    height,
	}
}

func (d Detail) Init() tea.Cmd { return nil }

func (d Detail) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		d.width = msg.Width
		d.height = msg.Height
		d.viewport.Width = msg.Width
		d.viewport.Height = msg.Height - 8
		return d, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "q":
			return d, func() tea.Msg { return BackToListMsg{} }
		case "e":
			return d, func() tea.Msg { return EditEntryMsg{Entry: d.entry} }
		}
	}

	var cmd tea.Cmd
	d.viewport, cmd = d.viewport.Update(msg)
	return d, cmd
}

func (d Detail) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#7C3AED")).
		MarginBottom(1)

	metaStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280"))

	header := titleStyle.Render(d.entry.Title) + "\n" +
		metaStyle.Render(fmt.Sprintf("[%s] %s  Created: %s", d.entry.ID, d.entry.Category, d.entry.CreatedAt))

	return header + "\n\n" + d.viewport.View() + "\n" + d.statusBar.View()
}

func formatEntryContent(entry *vault.Entry, data []byte) string {
	if entry.Category == vault.CategoryCredentials {
		return formatCredential(data)
	}
	return string(data)
}

func formatCredential(data []byte) string {
	var cred vault.Credential
	if err := json.Unmarshal(data, &cred); err != nil {
		return string(data)
	}

	label := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#06B6D4")).
		Width(18)
	value := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E5E7EB"))

	var b strings.Builder
	field := func(name, val string) {
		if val != "" {
			b.WriteString(label.Render(name) + value.Render(val) + "\n")
		}
	}

	field("Service:", cred.Service)
	field("URL:", cred.URL)
	field("Username:", cred.Username)
	field("Password:", cred.Password)
	if cred.TOTPSecret != "" {
		field("TOTP Secret:", cred.TOTPSecret)
	}
	if len(cred.RecoveryCodes) > 0 {
		field("Recovery Codes:", strings.Join(cred.RecoveryCodes, ", "))
	}
	if cred.Notes != "" {
		b.WriteString("\n" + label.Render("Notes:") + "\n" + value.Render(cred.Notes) + "\n")
	}

	return b.String()
}
