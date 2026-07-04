package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/olemoudi/kawarimi/internal/config"
	"github.com/olemoudi/kawarimi/internal/selfupdate"
	"github.com/spf13/cobra"
)

var (
	updateCheckOnly bool
	updateYes       bool
)

func init() {
	updateCmd.Flags().BoolVar(&updateCheckOnly, "check", false, "Only check whether a newer version is available")
	updateCmd.Flags().BoolVarP(&updateYes, "yes", "y", false, "Install without asking for confirmation")
	rootCmd.AddCommand(updateCmd)
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update kawarimi to the latest signed release",
	Long: `Checks GitHub for a newer kawarimi release, verifies its Ed25519 signature and
checksum, and replaces this binary. Only the owner tool self-updates — a recipient
opening a vault package never does.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 2*time.Minute)
		defer cancel()

		rel, available, err := selfupdate.Latest(ctx, version)
		if err != nil {
			return fmt.Errorf("checking for updates: %w", err)
		}
		if !available {
			fmt.Printf("kawarimi is up to date (v%s).\n", version)
			return nil
		}

		fmt.Printf("A newer version is available: v%s (you have v%s).\n", rel.Version, version)
		if rel.HTMLURL != "" {
			fmt.Printf("Release notes: %s\n", rel.HTMLURL)
		}
		if updateCheckOnly {
			fmt.Println("Run 'kawarimi update' to install it.")
			return nil
		}

		if !updateYes {
			reader := bufio.NewReader(os.Stdin)
			fmt.Print("Download and install it now? [y/N]: ")
			line, _ := reader.ReadString('\n')
			if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "y") {
				fmt.Println("Update cancelled.")
				return nil
			}
		}

		fmt.Println("Downloading and verifying the update…")
		if err := selfupdate.Apply(ctx, rel); err != nil {
			return fmt.Errorf("installing the update: %w", err)
		}
		fmt.Printf("Updated to v%s. Restart kawarimi to use the new version.\n", rel.Version)
		fmt.Println("Then run 'kawarimi switch verify' — if it reports the cloud automation as")
		fmt.Println("outdated, run 'kawarimi switch seed' so the improvements reach your switch.")
		return nil
	},
}

// printUpdateHintFromCache prints a one-line "update available" note from the cached
// check (no network). Best-effort and silent when nothing is cached.
func printUpdateHintFromCache() {
	appDir, err := config.AppDirPath()
	if err != nil {
		return
	}
	if rel, available, _ := selfupdate.CachedLatest(appDir, version); available {
		fmt.Fprintf(os.Stderr, "\nA newer version of kawarimi is available (v%s) — run 'kawarimi update'.\n", rel.Version)
	}
}

// refreshUpdateHint does a short bounded check, caches it, and prints a hint. Used
// by informational commands (status) where a brief network call is acceptable.
func refreshUpdateHint() {
	appDir, err := config.AppDirPath()
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if rel, available, err := selfupdate.RefreshCache(ctx, appDir, version); err == nil && available {
		fmt.Fprintf(os.Stderr, "\nA newer version of kawarimi is available (v%s) — run 'kawarimi update'.\n", rel.Version)
	}
}
