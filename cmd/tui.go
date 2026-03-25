package cmd

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/olemoudi/kawarimi/internal/tui"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(tuiCmd)
}

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Launch the interactive terminal UI",
	Long:  "Opens an interactive terminal dashboard for managing your vault, entries, config, and dead man's switch.",
	RunE: func(cmd *cobra.Command, args []string) error {
		app := tui.New()
		p := tea.NewProgram(app, tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			return fmt.Errorf("TUI error: %w", err)
		}
		return nil
	},
}
