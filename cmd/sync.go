package cmd

import (
	"fmt"

	"github.com/olemoudi/kawarimi/internal/config"
	gosync "github.com/olemoudi/kawarimi/internal/sync"
	"github.com/spf13/cobra"
)

func init() {
	syncCmd.Flags().Bool("git", false, "Sync to git remote")
	syncCmd.Flags().String("usb", "", "Sync to USB path")
	rootCmd.AddCommand(syncCmd)
}

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync vault to configured targets",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		gitFlag, _ := cmd.Flags().GetBool("git")
		usbFlag, _ := cmd.Flags().GetString("usb")

		// If no flags specified, use config defaults
		if !gitFlag && usbFlag == "" {
			if cfg.AutoSync.Git && cfg.SyncTargets.GitRemote != "" {
				gitFlag = true
			}
			if cfg.AutoSync.USB && cfg.SyncTargets.USBPath != "" {
				usbFlag = cfg.SyncTargets.USBPath
			}
		}

		if !gitFlag && usbFlag == "" {
			return fmt.Errorf("specify --git and/or --usb=<path>, or configure auto_sync in config")
		}

		if gitFlag {
			remote := cfg.SyncTargets.GitRemote
			gs := gosync.NewGitSync(cfg.VaultDir, remote, "")
			fmt.Println("Syncing to git...")
			if err := gs.Sync(); err != nil {
				return fmt.Errorf("git sync: %w", err)
			}
		}

		if usbFlag != "" {
			us := gosync.NewUSBSync(cfg.VaultDir, usbFlag)
			fmt.Println("Syncing to USB...")
			if err := us.Sync(); err != nil {
				return fmt.Errorf("USB sync: %w", err)
			}
		}

		return nil
	},
}
