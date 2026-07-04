package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/olemoudi/kawarimi/internal/config"
	"github.com/olemoudi/kawarimi/internal/deadswitch"
	gosync "github.com/olemoudi/kawarimi/internal/sync"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(checkinCmd)
}

var checkinCmd = &cobra.Command{
	Use:   "checkin",
	Short: "Record a check-in (resets dead man's switch timer)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		// Check if switch was triggered
		appDir, err := config.AppDirPath()
		if err != nil {
			return err
		}
		triggeredPath := filepath.Join(appDir, "switch-triggered")
		if _, err := os.Stat(triggeredPath); err == nil {
			printTriggeredWarning(cfg.VaultDir)
			fmt.Println()
		}

		now := time.Now().UTC()
		targets, err := checkinTargets(cfg)
		if err != nil {
			return err
		}

		pushed, checkinErr := deadswitch.RecordCheckin(targets, now)
		// A local-write failure is always fatal (a phantom check-in is dangerous);
		// with no cloud DMS configured, the local write is the only failure path.
		if checkinErr != nil && (cfg.SyncTargets.DMSRemote == "" || errors.Is(checkinErr, deadswitch.ErrLocalCheckin)) {
			return checkinErr
		}

		fmt.Printf("Check-in recorded: %s\n", now.Format(time.RFC3339))
		nextDue := now.Add(time.Duration(cfg.CheckinInterval) * 24 * time.Hour)
		fmt.Printf("Next check-in due by: %s\n", nextDue.Format("2006-01-02"))

		if cfg.SyncTargets.DMSRemote != "" {
			if pushed {
				fmt.Println("Cloud dead man's switch updated.")
			} else {
				fmt.Fprintln(os.Stderr)
				fmt.Fprintln(os.Stderr, "WARNING: the cloud dead man's switch did NOT receive this check-in.")
				fmt.Fprintln(os.Stderr, "Once overdue it may fire while you are alive. Fix connectivity and run")
				fmt.Fprintln(os.Stderr, "'kawarimi checkin' again, or 'kawarimi switch verify'.")
				if checkinErr != nil {
					fmt.Fprintf(os.Stderr, "Cause: %v\n", checkinErr)
				}
				// Exit non-zero so automation (cron/scripts) notices the cloud gap.
				return fmt.Errorf("cloud check-in did not reach the dead man's switch")
			}
		}

		// Auto-push the vault itself to its git remote (keeps legacy in-vault
		// workflows and off-site backups current).
		if cfg.SyncTargets.GitRemote != "" {
			fmt.Println("Pushing vault to git...")
			gs := gosync.NewGitSync(cfg.VaultDir, cfg.SyncTargets.GitRemote, "")
			if err := gs.Sync(); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: git sync failed: %v\n", err)
				fmt.Fprintln(os.Stderr, "Run 'kawarimi sync --git' to retry.")
			}
		}

		printUpdateHintFromCache()
		return nil
	},
}
