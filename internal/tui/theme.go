package tui

import "github.com/charmbracelet/lipgloss"

// Colors
var (
	ColorPrimary   = lipgloss.Color("#7C3AED") // violet
	ColorSecondary = lipgloss.Color("#06B6D4") // cyan
	ColorSuccess   = lipgloss.Color("#10B981") // green
	ColorWarning   = lipgloss.Color("#F59E0B") // amber
	ColorDanger    = lipgloss.Color("#EF4444") // red
	ColorMuted     = lipgloss.Color("#6B7280") // gray
	ColorText      = lipgloss.Color("#E5E7EB") // light gray
	ColorBg        = lipgloss.Color("#111827") // dark
	ColorBorder    = lipgloss.Color("#374151") // gray border
)

// Styles
var (
	StyleTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPrimary).
			MarginBottom(1)

	StyleSubtitle = lipgloss.NewStyle().
			Foreground(ColorSecondary).
			Bold(true)

	StyleMuted = lipgloss.NewStyle().
			Foreground(ColorMuted)

	StyleSuccess = lipgloss.NewStyle().
			Foreground(ColorSuccess)

	StyleWarning = lipgloss.NewStyle().
			Foreground(ColorWarning)

	StyleDanger = lipgloss.NewStyle().
			Foreground(ColorDanger).
			Bold(true)

	StyleBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorder).
			Padding(1, 2)

	StyleActiveTab = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPrimary).
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(ColorPrimary).
			Padding(0, 2)

	StyleInactiveTab = lipgloss.NewStyle().
				Foreground(ColorMuted).
				Padding(0, 2)

	StyleStatusBar = lipgloss.NewStyle().
			Foreground(ColorMuted).
			MarginTop(1)

	StyleLabel = lipgloss.NewStyle().
			Foreground(ColorSecondary).
			Bold(true).
			Width(20)

	StyleValue = lipgloss.NewStyle().
			Foreground(ColorText)

	StyleKey = lipgloss.NewStyle().
			Foreground(ColorPrimary).
			Bold(true)

	StyleHelp = lipgloss.NewStyle().
			Foreground(ColorMuted)
)
