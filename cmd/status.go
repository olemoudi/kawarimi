package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/olemoudi/kawarimi/internal/config"
	"github.com/olemoudi/kawarimi/internal/vault"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(statusCmd)
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show vault and switch status",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		fmt.Printf("Vault: %s\n", cfg.VaultDir)

		// Check if manifest exists (don't need passphrase for basic status)
		manifestPath := filepath.Join(cfg.VaultDir, vault.ManifestFile)
		info, err := os.Stat(manifestPath)
		if err != nil {
			fmt.Println("Status: NOT INITIALIZED")
			return nil
		}
		fmt.Printf("Last modified: %s\n", info.ModTime().Format(time.RFC3339))

		// Check last check-in
		checkinPath := filepath.Join(cfg.VaultDir, vault.LastCheckinFile)
		checkinData, err := os.ReadFile(checkinPath)
		if err == nil {
			ts := strings.TrimSpace(string(checkinData))
			checkinTime, err := time.Parse(time.RFC3339, ts)
			if err == nil {
				daysSince := int(time.Since(checkinTime).Hours() / 24)
				fmt.Printf("Last check-in: %s (%d days ago)\n", ts, daysSince)
				fmt.Printf("Check-in interval: %d days\n", cfg.CheckinInterval)
				if daysSince > cfg.CheckinInterval {
					fmt.Printf("WARNING: Check-in overdue by %d days!\n", daysSince-cfg.CheckinInterval)
				}
			}
		} else {
			fmt.Println("Last check-in: never")
		}

		// Check switch triggered marker
		appDir, _ := config.AppDirPath()
		if _, err := os.Stat(filepath.Join(appDir, "switch-triggered")); err == nil {
			fmt.Println()
			printTriggeredWarning(cfg.VaultDir)
		}

		// Cloud dead man's switch
		if cfg.SyncTargets.DMSRemote != "" {
			fmt.Printf("\nCloud DMS: configured (%s)\n", cfg.SyncTargets.DMSRemote)
			fmt.Println("  Run 'kawarimi switch verify' to confirm it is armed and current.")
		} else {
			fmt.Println("\nCloud DMS: not configured")
		}

		fmt.Printf("\nSync targets:\n")
		if cfg.SyncTargets.GitRemote != "" {
			fmt.Printf("  Git: %s\n", cfg.SyncTargets.GitRemote)
		} else {
			fmt.Println("  Git: not configured")
		}
		if cfg.SyncTargets.USBPath != "" {
			fmt.Printf("  USB: %s\n", cfg.SyncTargets.USBPath)
		} else {
			fmt.Println("  USB: not configured")
		}

		return nil
	},
}
