package screens

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/olemoudi/kawarimi/internal/config"
	"github.com/olemoudi/kawarimi/internal/deadswitch"
	"github.com/olemoudi/kawarimi/internal/tui/components"
	"github.com/olemoudi/kawarimi/internal/vault"
)

// CheckInDoneMsg is sent after a successful check-in.
type CheckInDoneMsg struct{}

// Switch is the dead man's switch dashboard.
type Switch struct {
	cfg          *config.Config
	switchCfg    *deadswitch.SwitchConfig
	configured   bool
	lastCheckin  time.Time
	daysSince    int
	triggered    bool
	statusBar    components.StatusBar
	toast        components.Toast
	err          string
	width        int
	height       int
}

// NewSwitch creates the switch dashboard.
func NewSwitch(cfg *config.Config, width, height int) Switch {
	s := Switch{
		cfg:   cfg,
		width: width,
		height: height,
		statusBar: components.NewStatusBar(
			components.Hint("c", "check in"),
		),
		toast: components.NewToast(),
	}

	appDir, err := config.AppDirPath()
	if err == nil {
		s.configured = deadswitch.IsSwitchConfigured(appDir)
		if s.configured {
			s.switchCfg, _ = deadswitch.LoadSwitchConfig(appDir)
		}
		if _, err := os.Stat(filepath.Join(appDir, "switch-triggered")); err == nil {
			s.triggered = true
		}
	}

	if lastCheckin, err := deadswitch.ReadLastCheckin(cfg.VaultDir); err == nil {
		s.lastCheckin = lastCheckin
		s.daysSince = int(time.Since(lastCheckin).Hours() / 24)
	} else {
		s.daysSince = -1
	}

	return s
}

func (s Switch) Init() tea.Cmd { return nil }

func (s Switch) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		return s, nil

	case CheckInDoneMsg:
		s.lastCheckin = time.Now().UTC()
		s.daysSince = 0
		return s, s.toast.Show("Checked in successfully!", components.ToastSuccess)

	case UnlockErrorMsg:
		s.err = msg.Err.Error()
		return s, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "c":
			return s, s.doCheckIn()
		}
	}

	var cmd tea.Cmd
	s.toast, cmd = s.toast.Update(msg)
	return s, cmd
}

func (s Switch) doCheckIn() tea.Cmd {
	return func() tea.Msg {
		checkinPath := filepath.Join(s.cfg.VaultDir, vault.LastCheckinFile)
		now := time.Now().UTC().Format(time.RFC3339)
		if err := os.WriteFile(checkinPath, []byte(now), 0600); err != nil {
			return UnlockErrorMsg{Err: fmt.Errorf("check-in failed: %w", err)}
		}
		return CheckInDoneMsg{}
	}
}

func (s Switch) View() string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#7C3AED")).
		MarginBottom(1).
		Render("Dead Man's Switch")

	label := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#06B6D4")).
		Width(22)
	value := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E5E7EB"))

	var b strings.Builder

	// Configured status
	if s.configured {
		b.WriteString(label.Render("Status:") + lipgloss.NewStyle().
			Foreground(lipgloss.Color("#10B981")).Bold(true).Render("CONFIGURED") + "\n")
	} else {
		b.WriteString(label.Render("Status:") + lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F59E0B")).Bold(true).Render("NOT CONFIGURED") + "\n")
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).
			Render("  Run 'kawarimi switch setup' to configure.") + "\n")
	}

	// Triggered alert
	if s.triggered {
		b.WriteString("\n" + lipgloss.NewStyle().
			Bold(true).Foreground(lipgloss.Color("#EF4444")).
			Render("  ALERT: Switch has been TRIGGERED!") + "\n")
	}

	// Last check-in
	b.WriteString("\n")
	if s.daysSince >= 0 {
		checkinStr := s.lastCheckin.Format("2006-01-02 15:04 UTC")
		b.WriteString(label.Render("Last check-in:") + value.Render(checkinStr) + "\n")

		daysStr := fmt.Sprintf("%d days ago", s.daysSince)
		daysStyle := value
		if s.daysSince > s.cfg.CheckinInterval {
			daysStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444")).Bold(true)
			daysStr += " (OVERDUE)"
		} else if s.daysSince > s.cfg.CheckinInterval/2 {
			daysStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B"))
		}
		b.WriteString(label.Render("") + daysStyle.Render(daysStr) + "\n")
	} else {
		b.WriteString(label.Render("Last check-in:") + lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F59E0B")).Render("Never") + "\n")
	}

	b.WriteString(label.Render("Check-in interval:") +
		value.Render(fmt.Sprintf("%d days", s.cfg.CheckinInterval)) + "\n")

	// Escalation thresholds
	if s.switchCfg != nil {
		b.WriteString("\n" + lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color("#06B6D4")).Render("Escalation Thresholds:") + "\n")
		b.WriteString(label.Render("  Warning 1:") +
			value.Render(fmt.Sprintf("%d days", s.switchCfg.Warning1Days)) + "\n")
		b.WriteString(label.Render("  Warning 2:") +
			value.Render(fmt.Sprintf("%d days", s.switchCfg.Warning2Days)) + "\n")
		b.WriteString(label.Render("  Final release:") +
			value.Render(fmt.Sprintf("%d days", s.switchCfg.FinalDays)) + "\n")

		// Ping channels
		b.WriteString("\n" + lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color("#06B6D4")).Render("Ping Channels:") + "\n")
		if len(s.switchCfg.PingChannels) > 0 {
			b.WriteString(label.Render("  Channels:") +
				value.Render(strings.Join(s.switchCfg.PingChannels, ", ")) + "\n")
		} else {
			b.WriteString(label.Render("  Channels:") + value.Render("email (default)") + "\n")
		}
		if s.switchCfg.TelegramBotToken != "" {
			b.WriteString(label.Render("  Telegram:") +
				lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981")).Render("configured") + "\n")
		}
		if s.switchCfg.IMAPServer != "" {
			b.WriteString(label.Render("  Email reply:") +
				lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981")).Render("configured ("+s.switchCfg.IMAPServer+")") + "\n")
		}

		// Mnemonic delivery
		b.WriteString("\n" + lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color("#06B6D4")).Render("Delivery:") + "\n")
		mnemonicMode := s.switchCfg.MnemonicDelivery
		if mnemonicMode == "" {
			mnemonicMode = "email"
		}
		b.WriteString(label.Render("  Mnemonic via:") + value.Render(mnemonicMode) + "\n")
		if s.switchCfg.DeliveryInstructions != "" {
			b.WriteString(label.Render("  Vault delivery:") + value.Render("custom instructions") + "\n")
		}

		// Recipients
		b.WriteString("\n" + lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color("#06B6D4")).Render("Recipients:") + "\n")
		for _, r := range s.switchCfg.Recipients {
			b.WriteString("  " + value.Render(r) + "\n")
		}
	}

	var errView string
	if s.err != "" {
		errView = "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444")).Render(s.err)
	}

	return title + "\n\n" + b.String() + errView + "\n" + s.toast.View() + "\n" + s.statusBar.View()
}
