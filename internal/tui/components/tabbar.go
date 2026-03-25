package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	activeTab = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7C3AED")).
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(lipgloss.Color("#7C3AED")).
			Padding(0, 2)

	inactiveTab = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6B7280")).
			Padding(0, 2)

	tabGap = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#374151")).
		SetString("|")
)

// TabBar renders a horizontal tab bar.
type TabBar struct {
	Tabs      []string
	ActiveTab int
}

// NewTabBar creates a tab bar with the given tab names.
func NewTabBar(tabs ...string) TabBar {
	return TabBar{Tabs: tabs}
}

// View renders the tab bar.
func (t TabBar) View() string {
	var tabs []string
	for i, tab := range t.Tabs {
		if i == t.ActiveTab {
			tabs = append(tabs, activeTab.Render(tab))
		} else {
			tabs = append(tabs, inactiveTab.Render(tab))
		}
		if i < len(t.Tabs)-1 {
			tabs = append(tabs, tabGap.String())
		}
	}
	return strings.Join(tabs, "")
}

// Next moves to the next tab.
func (t *TabBar) Next() {
	t.ActiveTab = (t.ActiveTab + 1) % len(t.Tabs)
}

// Prev moves to the previous tab.
func (t *TabBar) Prev() {
	t.ActiveTab = (t.ActiveTab - 1 + len(t.Tabs)) % len(t.Tabs)
}
