package cmd

import (
	"github.com/olemoudi/kawarimi/internal/gui"
	"github.com/spf13/cobra"
)

var (
	guiPort      int
	guiNoBrowser bool
	guiSource    string
)

func init() {
	guiCmd.Flags().IntVar(&guiPort, "port", 0, "Port to listen on (0 = random, loopback only)")
	guiCmd.Flags().BoolVar(&guiNoBrowser, "no-browser", false, "Do not open a browser automatically")
	guiCmd.Flags().StringVar(&guiSource, "source", "", "kawarimi source checkout for building recipient packages (default: auto-detect)")
	rootCmd.AddCommand(guiCmd)
}

var guiCmd = &cobra.Command{
	Use:   "gui",
	Short: "Launch the owner console in your web browser",
	Long: `Starts a local web server (bound to 127.0.0.1 only) and opens the kawarimi
owner console in your default browser: a wizard to create a vault, arm the cloud
dead man's switch, and build the recipient package, plus day-to-day management.

The server is loopback-only, protected by a per-session token, and shuts down when
you close the page or press Ctrl-C.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return gui.Run(gui.Options{
			Port:      guiPort,
			NoBrowser: guiNoBrowser,
			Version:   version,
			SourceDir: guiSource,
		})
	},
}
