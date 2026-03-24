package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/olemoudi/kawarimi/internal/config"
	gosync "github.com/olemoudi/kawarimi/internal/sync"
	"github.com/olemoudi/kawarimi/internal/vault"
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
			fmt.Println("WARNING: The dead man's switch has been TRIGGERED.")
			fmt.Println("Your passphrase may have been sent to recipients.")
			fmt.Println("Run 'kawarimi passwd' to change your passphrase.")
			fmt.Println()
		}

		// Write check-in timestamp to vault
		now := time.Now().UTC()
		checkinPath := filepath.Join(cfg.VaultDir, vault.LastCheckinFile)
		if err := os.WriteFile(checkinPath, []byte(now.Format(time.RFC3339)+"\n"), 0644); err != nil {
			return fmt.Errorf("writing check-in: %w", err)
		}

		fmt.Printf("Check-in recorded: %s\n", now.Format(time.RFC3339))

		nextDue := now.Add(time.Duration(cfg.CheckinInterval) * 24 * time.Hour)
		fmt.Printf("Next check-in due by: %s\n", nextDue.Format("2006-01-02"))

		// Auto-push to git if configured
		if cfg.SyncTargets.GitRemote != "" {
			fmt.Println("Pushing check-in to git...")
			gs := gosync.NewGitSync(cfg.VaultDir, cfg.SyncTargets.GitRemote, "")
			if err := gs.Sync(); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: git sync failed: %v\n", err)
				fmt.Fprintln(os.Stderr, "Run 'kawarimi sync --git' to retry.")
			}
		}

		return nil
	},
}
