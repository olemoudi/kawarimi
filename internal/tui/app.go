package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/olemoudi/kawarimi/internal/config"
	"github.com/olemoudi/kawarimi/internal/tui/components"
	"github.com/olemoudi/kawarimi/internal/tui/screens"
	"github.com/olemoudi/kawarimi/internal/vault"
)

// Tab identifies the active tab.
type Tab int

const (
	TabDashboard Tab = iota
	TabEntries
	TabConfig
	TabSwitch
)

var tabNames = []string{"Dashboard", "Entries", "Config", "Switch"}

// App is the root TUI model.
type App struct {
	// Auth state
	vault    *vault.Vault
	header   *vault.Header
	cfg      *config.Config
	unlocked bool

	// Navigation
	activeTab Tab
	screen    tea.Model // active screen
	subScreen tea.Model // drill-down screen (detail, editor)

	// Layout
	tabBar components.TabBar
	toast  components.Toast
	width  int
	height int
}

// New creates the root TUI app.
func New() App {
	return App{
		tabBar: components.NewTabBar(tabNames...),
		toast:  components.NewToast(),
		screen: screens.NewUnlock(),
	}
}

func (a App) Init() tea.Cmd {
	return a.screen.Init()
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		// Forward to active screen
		if a.subScreen != nil {
			var cmd tea.Cmd
			a.subScreen, cmd = a.subScreen.Update(msg)
			return a, cmd
		}
		var cmd tea.Cmd
		a.screen, cmd = a.screen.Update(msg)
		return a, cmd

	case screens.VaultUnlockedMsg:
		a.vault = msg.Vault
		a.header = msg.Header
		a.cfg = msg.Config
		a.unlocked = true
		a.switchToTab(TabDashboard)
		return a, nil

	case screens.UnlockErrorMsg:
		if !a.unlocked {
			var cmd tea.Cmd
			a.screen, cmd = a.screen.Update(msg)
			return a, cmd
		}
		return a, a.toast.Show(msg.Err.Error(), components.ToastError)

	case screens.ViewEntryMsg:
		a.subScreen = screens.NewDetail(msg.Entry, a.vault, a.width, a.height-4)
		return a, nil

	case screens.BackToListMsg:
		a.subScreen = nil
		return a, nil

	case screens.NewEntryMsg:
		a.subScreen = screens.NewCategorySelector(a.vault, a.width, a.height-4)
		return a, nil

	case screens.EditEntryMsg:
		data, err := a.vault.ShowEntry(msg.Entry)
		if err != nil {
			return a, a.toast.Show(fmt.Sprintf("Decrypt failed: %v", err), components.ToastError)
		}
		if msg.Entry.Category == vault.CategoryCredentials {
			a.subScreen = screens.NewCredentialEditor(a.vault, msg.Entry, data, a.width, a.height-4)
		} else {
			a.subScreen = screens.NewNoteEditor(a.vault, msg.Entry, string(data), a.width, a.height-4)
		}
		return a, nil

	case screens.EntrySavedMsg:
		a.subScreen = nil
		action := "updated"
		if msg.IsNew {
			action = "created"
		}
		// Refresh the entries list
		a.switchToTab(TabEntries)
		return a, a.toast.Show(fmt.Sprintf("Entry %s: %s", action, msg.Entry.Title), components.ToastSuccess)

	case screens.DeleteEntryMsg:
		_, err := a.vault.RemoveEntry(msg.Entry.ID)
		if err != nil {
			return a, a.toast.Show(fmt.Sprintf("Delete failed: %v", err), components.ToastError)
		}
		// Refresh entries
		a.switchToTab(TabEntries)
		return a, a.toast.Show(fmt.Sprintf("Deleted: %s", msg.Entry.Title), components.ToastSuccess)

	case screens.ConfigSavedMsg:
		return a, a.toast.Show("Settings saved", components.ToastSuccess)

	case screens.CheckInDoneMsg:
		if a.subScreen != nil {
			var cmd tea.Cmd
			a.subScreen, cmd = a.subScreen.Update(msg)
			return a, cmd
		}
		var cmd tea.Cmd
		a.screen, cmd = a.screen.Update(msg)
		return a, cmd

	case tea.KeyMsg:
		if !a.unlocked {
			var cmd tea.Cmd
			a.screen, cmd = a.screen.Update(msg)
			return a, cmd
		}

		// Global keys when unlocked
		if a.subScreen == nil {
			switch msg.String() {
			case "ctrl+c":
				return a, tea.Quit
			case "q":
				return a, tea.Quit
			case "tab":
				a.tabBar.Next()
				a.activeTab = Tab(a.tabBar.ActiveTab)
				a.switchToTab(a.activeTab)
				return a, nil
			case "shift+tab":
				a.tabBar.Prev()
				a.activeTab = Tab(a.tabBar.ActiveTab)
				a.switchToTab(a.activeTab)
				return a, nil
			case "1":
				a.setTab(TabDashboard)
				return a, nil
			case "2":
				a.setTab(TabEntries)
				return a, nil
			case "3":
				a.setTab(TabConfig)
				return a, nil
			case "4":
				a.setTab(TabSwitch)
				return a, nil
			}
		}
	}

	// Forward toast messages
	var toastCmd tea.Cmd
	a.toast, toastCmd = a.toast.Update(msg)

	// Forward to sub-screen or main screen
	var screenCmd tea.Cmd
	if a.subScreen != nil {
		a.subScreen, screenCmd = a.subScreen.Update(msg)
	} else if a.screen != nil {
		a.screen, screenCmd = a.screen.Update(msg)
	}

	return a, tea.Batch(toastCmd, screenCmd)
}

func (a *App) setTab(tab Tab) {
	a.tabBar.ActiveTab = int(tab)
	a.activeTab = tab
	a.switchToTab(tab)
}

func (a *App) switchToTab(tab Tab) {
	a.subScreen = nil
	contentHeight := a.height - 4 // account for tab bar + status

	switch tab {
	case TabDashboard:
		a.screen = screens.NewDashboard(a.vault, a.cfg, a.width, contentHeight)
	case TabEntries:
		a.screen = screens.NewEntries(a.vault, a.width, contentHeight)
	case TabConfig:
		a.screen = screens.NewConfig(a.cfg, a.width, contentHeight)
	case TabSwitch:
		a.screen = screens.NewSwitch(a.cfg, a.width, contentHeight)
	}
}

func (a App) View() string {
	if !a.unlocked {
		return a.screen.View()
	}

	// Tab bar
	tabView := a.tabBar.View()

	// Main content
	var content string
	if a.subScreen != nil {
		content = a.subScreen.View()
	} else if a.screen != nil {
		content = a.screen.View()
	}

	// Toast
	toastView := a.toast.View()

	// Bottom help
	help := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).
		Render("tab: switch tabs | 1-4: jump to tab | q: quit")

	parts := []string{tabView, "", content}
	if toastView != "" {
		parts = append(parts, "", toastView)
	}
	parts = append(parts, "", help)

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}
