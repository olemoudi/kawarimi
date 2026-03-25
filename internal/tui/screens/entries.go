package screens

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/olemoudi/kawarimi/internal/tui/components"
	"github.com/olemoudi/kawarimi/internal/vault"
)

// ViewEntryMsg requests viewing a specific entry.
type ViewEntryMsg struct{ Entry *vault.Entry }

// NewEntryMsg requests creating a new entry.
type NewEntryMsg struct{ Category vault.Category }

// DeleteEntryMsg requests deleting an entry.
type DeleteEntryMsg struct{ Entry *vault.Entry }

// entryItem wraps a vault entry for the list component.
type entryItem struct{ entry *vault.Entry }

func (e entryItem) FilterValue() string {
	return e.entry.Title + " " + string(e.entry.Category) + " " + strings.Join(e.entry.Tags, " ")
}

// entryDelegate renders list items.
type entryDelegate struct{}

func (d entryDelegate) Height() int                             { return 2 }
func (d entryDelegate) Spacing() int                            { return 0 }
func (d entryDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d entryDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	ei, ok := item.(entryItem)
	if !ok {
		return
	}
	e := ei.entry

	isSelected := index == m.Index()

	catStyle := lipgloss.NewStyle().Width(13)
	switch e.Category {
	case vault.CategoryNotes:
		catStyle = catStyle.Foreground(lipgloss.Color("#06B6D4"))
	case vault.CategoryCredentials:
		catStyle = catStyle.Foreground(lipgloss.Color("#F59E0B"))
	case vault.CategoryDocuments:
		catStyle = catStyle.Foreground(lipgloss.Color("#10B981"))
	}

	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#E5E7EB"))
	idStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))

	if isSelected {
		titleStyle = titleStyle.Bold(true).Foreground(lipgloss.Color("#7C3AED"))
	}

	line1 := titleStyle.Render(e.Title)
	line2 := catStyle.Render(string(e.Category)) + idStyle.Render("["+e.ID+"]")

	cursor := "  "
	if isSelected {
		cursor = lipgloss.NewStyle().Foreground(lipgloss.Color("#7C3AED")).Render("> ")
	}

	fmt.Fprintf(w, "%s%s\n%s  %s", cursor, line1, "  ", line2)
}

// Entries is the entry browser screen.
type Entries struct {
	list      list.Model
	vault     *vault.Vault
	confirm   components.Confirm
	statusBar components.StatusBar
	width     int
	height    int
}

// NewEntries creates the entry browser.
func NewEntries(v *vault.Vault, width, height int) Entries {
	items := make([]list.Item, len(v.Manifest.Entries))
	for i, e := range v.Manifest.Entries {
		items[i] = entryItem{entry: e}
	}

	delegate := entryDelegate{}
	l := list.New(items, delegate, width, height-6)
	l.Title = "Vault Entries"
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(false)
	l.Styles.Title = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7C3AED"))

	sb := components.NewStatusBar(
		components.Hint("enter", "view"),
		components.Hint("n", "new"),
		components.Hint("e", "edit"),
		components.Hint("d", "delete"),
		components.Hint("/", "search"),
	)

	return Entries{
		list:      l,
		vault:     v,
		statusBar: sb,
		width:     width,
		height:    height,
	}
}

func (e Entries) Init() tea.Cmd { return nil }

func (e Entries) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if e.confirm.Active {
		var cmd tea.Cmd
		e.confirm, cmd = e.confirm.Update(msg)
		return e, cmd
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		e.width = msg.Width
		e.height = msg.Height
		e.list.SetSize(msg.Width, msg.Height-6)
		return e, nil

	case components.ConfirmResult:
		if msg.Confirmed && msg.Tag == "delete" {
			if item, ok := e.list.SelectedItem().(entryItem); ok {
				return e, func() tea.Msg { return DeleteEntryMsg{Entry: item.entry} }
			}
		}
		return e, nil

	case tea.KeyMsg:
		// Don't intercept keys during filtering
		if e.list.FilterState() == list.Filtering {
			break
		}

		switch msg.String() {
		case "enter":
			if item, ok := e.list.SelectedItem().(entryItem); ok {
				return e, func() tea.Msg { return ViewEntryMsg{Entry: item.entry} }
			}
		case "n":
			return e, func() tea.Msg { return NewEntryMsg{} }
		case "e":
			if item, ok := e.list.SelectedItem().(entryItem); ok {
				return e, func() tea.Msg { return ViewEntryMsg{Entry: item.entry} }
			}
		case "d":
			if item, ok := e.list.SelectedItem().(entryItem); ok {
				e.confirm = components.NewConfirm(
					"Delete Entry",
					fmt.Sprintf("Delete %q? This cannot be undone.", item.entry.Title),
					"delete",
				)
				return e, nil
			}
		}
	}

	var cmd tea.Cmd
	e.list, cmd = e.list.Update(msg)
	return e, cmd
}

func (e Entries) View() string {
	if e.confirm.Active {
		return lipgloss.Place(e.width, e.height-4,
			lipgloss.Center, lipgloss.Center,
			e.confirm.View())
	}

	return e.list.View() + "\n" + e.statusBar.View()
}

// RefreshEntries updates the entry list from the vault.
func (e *Entries) RefreshEntries(v *vault.Vault) {
	e.vault = v
	items := make([]list.Item, len(v.Manifest.Entries))
	for i, entry := range v.Manifest.Entries {
		items[i] = entryItem{entry: entry}
	}
	e.list.SetItems(items)
}
