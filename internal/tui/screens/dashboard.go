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
	"github.com/olemoudi/kawarimi/internal/vault"
)

// Dashboard is the status overview screen.
type Dashboard struct {
	v         *vault.Vault
	cfg       *config.Config
	width     int
	height    int
}

// NewDashboard creates the status overview screen.
func NewDashboard(v *vault.Vault, cfg *config.Config, width, height int) Dashboard {
	return Dashboard{v: v, cfg: cfg, width: width, height: height}
}

func (d Dashboard) Init() tea.Cmd { return nil }

func (d Dashboard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		d.width = msg.Width
		d.height = msg.Height
	}
	return d, nil
}

func (d Dashboard) View() string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#7C3AED")).
		MarginBottom(1).
		Render("Dashboard")

	label := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#06B6D4")).
		Width(22)
	value := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E5E7EB"))

	var b strings.Builder

	// Vault info
	b.WriteString(sectionHeader("Vault") + "\n")
	b.WriteString(label.Render("Location:") + value.Render(d.cfg.VaultDir) + "\n")
	b.WriteString(label.Render("Entries:") + value.Render(fmt.Sprintf("%d", len(d.v.Manifest.Entries))) + "\n")

	// Count by category
	notes, creds, docs := 0, 0, 0
	for _, e := range d.v.Manifest.Entries {
		switch e.Category {
		case vault.CategoryNotes:
			notes++
		case vault.CategoryCredentials:
			creds++
		case vault.CategoryDocuments:
			docs++
		}
	}
	b.WriteString(label.Render("") + lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).
		Render(fmt.Sprintf("  %d notes, %d credentials, %d documents", notes, creds, docs)) + "\n")

	isV2 := vault.IsV2Vault(d.cfg.VaultDir)
	versionStr := "v1 (legacy)"
	if isV2 {
		versionStr = "v2 (multi-slot)"
	}
	b.WriteString(label.Render("Vault version:") + value.Render(versionStr) + "\n")

	// Check-in status
	b.WriteString("\n" + sectionHeader("Dead Man's Switch") + "\n")
	lastCheckin, err := deadswitch.ReadLastCheckin(d.cfg.VaultDir)
	if err == nil {
		daysSince := int(time.Since(lastCheckin).Hours() / 24)
		checkinStr := lastCheckin.Format("2006-01-02 15:04 UTC")
		b.WriteString(label.Render("Last check-in:") + value.Render(checkinStr) + "\n")

		daysStyle := value
		daysStr := fmt.Sprintf("%d days ago", daysSince)
		if daysSince > d.cfg.CheckinInterval {
			daysStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444")).Bold(true)
			daysStr += " (OVERDUE!)"
		} else if daysSince > d.cfg.CheckinInterval/2 {
			daysStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B"))
		} else {
			daysStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981"))
		}
		b.WriteString(label.Render("") + daysStyle.Render(daysStr) + "\n")
	} else {
		b.WriteString(label.Render("Last check-in:") +
			lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B")).Render("Never") + "\n")
	}

	appDir, _ := config.AppDirPath()
	if _, err := os.Stat(filepath.Join(appDir, "switch-triggered")); err == nil {
		b.WriteString("\n" + lipgloss.NewStyle().
			Bold(true).Foreground(lipgloss.Color("#EF4444")).
			Render("  ALERT: Dead man's switch has been TRIGGERED!") + "\n")
	}

	// Sync targets
	b.WriteString("\n" + sectionHeader("Sync") + "\n")
	if d.cfg.SyncTargets.GitRemote != "" {
		b.WriteString(label.Render("Git:") + value.Render(d.cfg.SyncTargets.GitRemote) + "\n")
	} else {
		b.WriteString(label.Render("Git:") + lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6B7280")).Render("not configured") + "\n")
	}
	if d.cfg.SyncTargets.USBPath != "" {
		b.WriteString(label.Render("USB:") + value.Render(d.cfg.SyncTargets.USBPath) + "\n")
	} else {
		b.WriteString(label.Render("USB:") + lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6B7280")).Render("not configured") + "\n")
	}

	return title + "\n\n" + b.String()
}

func sectionHeader(s string) string {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#7C3AED")).
		Render("--- " + s + " ---")
}
