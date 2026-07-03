package screens

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/olemoudi/kawarimi/internal/config"
	"github.com/olemoudi/kawarimi/internal/tui/components"
)

// ConfigSavedMsg is sent when config is saved.
type ConfigSavedMsg struct{}

// Config is the settings editor screen.
type Config struct {
	cfg       *config.Config
	fields    []configField
	focusIdx  int
	statusBar components.StatusBar
	toast     components.Toast
	err       string
	width     int
	height    int
}

type configField struct {
	label string
	input textinput.Model
}

// NewConfig creates the config editor.
func NewConfig(cfg *config.Config, width, height int) Config {
	makeField := func(label, placeholder, value string) configField {
		ti := textinput.New()
		ti.Placeholder = placeholder
		ti.Width = 50
		ti.SetValue(value)
		return configField{label: label, input: ti}
	}

	fields := []configField{
		makeField("Vault Directory", "/path/to/vault", cfg.VaultDir),
		makeField("Check-in Interval (days)", "7", strconv.Itoa(cfg.CheckinInterval)),
		makeField("Git Remote", "git@github.com:user/vault.git", cfg.SyncTargets.GitRemote),
		makeField("USB Path", "/mnt/usb/vault-backup", cfg.SyncTargets.USBPath),
	}
	fields[0].input.Focus()

	return Config{
		cfg:    cfg,
		fields: fields,
		statusBar: components.NewStatusBar(
			components.Hint("tab", "next field"),
			components.Hint("ctrl+s", "save"),
		),
		toast:  components.NewToast(),
		width:  width,
		height: height,
	}
}

func (c Config) Init() tea.Cmd { return nil }

func (c Config) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		c.width = msg.Width
		c.height = msg.Height
		return c, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "tab":
			c.fields[c.focusIdx].input.Blur()
			c.focusIdx = (c.focusIdx + 1) % len(c.fields)
			c.fields[c.focusIdx].input.Focus()
			return c, nil
		case "shift+tab":
			c.fields[c.focusIdx].input.Blur()
			c.focusIdx = (c.focusIdx - 1 + len(c.fields)) % len(c.fields)
			c.fields[c.focusIdx].input.Focus()
			return c, nil
		case "ctrl+s":
			return c, c.save()
		}
	}

	var cmd tea.Cmd
	c.toast, cmd = c.toast.Update(msg)
	if cmd != nil {
		return c, cmd
	}

	c.fields[c.focusIdx].input, cmd = c.fields[c.focusIdx].input.Update(msg)
	return c, cmd
}

func (c Config) save() tea.Cmd {
	return func() tea.Msg {
		c.cfg.VaultDir = strings.TrimSpace(c.fields[0].input.Value())

		interval, err := strconv.Atoi(strings.TrimSpace(c.fields[1].input.Value()))
		if err != nil || interval < 1 {
			return UnlockErrorMsg{Err: fmt.Errorf("check-in interval must be a positive number")}
		}
		c.cfg.CheckinInterval = interval
		c.cfg.SyncTargets.GitRemote = strings.TrimSpace(c.fields[2].input.Value())
		c.cfg.SyncTargets.USBPath = strings.TrimSpace(c.fields[3].input.Value())
		c.cfg.AutoSync.Git = c.cfg.SyncTargets.GitRemote != ""
		c.cfg.AutoSync.USB = c.cfg.SyncTargets.USBPath != ""

		if err := config.Save(c.cfg); err != nil {
			return UnlockErrorMsg{Err: fmt.Errorf("saving config: %w", err)}
		}
		return ConfigSavedMsg{}
	}
}

func (c Config) View() string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#7C3AED")).
		MarginBottom(1).
		Render("Settings")

	label := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#06B6D4")).
		Width(26)

	var b strings.Builder
	for _, f := range c.fields {
		b.WriteString(label.Render(f.label) + "\n")
		b.WriteString(f.input.View() + "\n\n")
	}

	var errView string
	if c.err != "" {
		errView = lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444")).Render(c.err) + "\n"
	}

	return title + "\n\n" + b.String() + errView + c.toast.View() + "\n" + c.statusBar.View()
}
