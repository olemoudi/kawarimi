package cmd

import (
	"fmt"

	"github.com/olemoudi/kawarimi/internal/demo"
	"github.com/olemoudi/kawarimi/internal/gui"
	"github.com/spf13/cobra"
)

var (
	demoPort      int
	demoNoBrowser bool
)

func init() {
	demoCmd.Flags().IntVar(&demoPort, "port", 0, "Port to listen on (0 = random, loopback only)")
	demoCmd.Flags().BoolVar(&demoNoBrowser, "no-browser", false, "Do not open a browser automatically")
	rootCmd.AddCommand(demoCmd)
}

var demoCmd = &cobra.Command{
	Use:   "demo",
	Short: "Try the whole lifecycle in a sandboxed browser demo",
	Long: `Starts a throwaway sandbox world — a real vault, a real armed dead man's
switch, mock email/Telegram/GitHub — and opens a browser "lifecycle theater"
where you can watch the switch arm, warn, release, and the vault be opened by a
recipient, with time-travel controls (days pass on click, not on the calendar).

Nothing real is contacted or sent: every actor is simulated in-process, the
sandbox lives in a temporary folder, and it is deleted when the demo exits. Your
real vault and configuration (if any) are untouched. This is an owner-side
command; the recipient path is unaffected.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Building the demo sandbox (a real vault + armed switch, all actors mocked)...")
		w, err := demo.NewWorld(demo.Options{Version: version})
		if err != nil {
			return fmt.Errorf("building demo world: %w", err)
		}
		defer w.Close()
		return gui.Run(gui.Options{
			Port:      demoPort,
			NoBrowser: demoNoBrowser,
			Version:   version,
			Demo:      w,
		})
	},
}
