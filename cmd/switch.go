package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/olemoudi/kawarimi/internal/config"
	"github.com/olemoudi/kawarimi/internal/crypto"
	"github.com/olemoudi/kawarimi/internal/deadswitch"
	gosync "github.com/olemoudi/kawarimi/internal/sync"
	"github.com/olemoudi/kawarimi/internal/vault"
	"github.com/spf13/cobra"
)

var switchSeedForce bool

func init() {
	rootCmd.AddCommand(switchCmd)
	switchCmd.AddCommand(switchSetupCmd)
	switchCmd.AddCommand(switchSeedCmd)
	switchCmd.AddCommand(switchTestCmd)
	switchCmd.AddCommand(switchDisableCmd)
	switchCmd.AddCommand(switchEvaluateCmd)

	switchSeedCmd.Flags().BoolVar(&switchSeedForce, "force", false, "force-push the DMS repo (overwrites remote history; use to repair a diverged repo)")
}

var switchCmd = &cobra.Command{
	Use:   "switch",
	Short: "Manage the dead man's switch",
}

var switchSetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Configure the dead man's switch",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		appDir, err := config.AppDirPath()
		if err != nil {
			return err
		}

		reader := bufio.NewReader(os.Stdin)
		switchCfg := deadswitch.DefaultSwitchConfig()

		fmt.Println("=== Dead Man's Switch Setup ===")
		fmt.Println()

		// SMTP Configuration
		fmt.Println("-- SMTP Configuration --")
		switchCfg.SMTPServer = promptLine(reader, "SMTP server (e.g., smtp.gmail.com): ")
		portStr := promptLine(reader, "SMTP port (default: 587): ")
		if portStr != "" {
			port, err := strconv.Atoi(portStr)
			if err != nil {
				return fmt.Errorf("invalid port: %s", portStr)
			}
			switchCfg.SMTPPort = port
		}
		switchCfg.SMTPUsername = promptLine(reader, "SMTP username: ")
		switchCfg.SMTPPassword, _ = crypto.PromptPassphrase("SMTP password: ")
		switchCfg.SenderEmail = promptLine(reader, "Sender email address: ")
		if switchCfg.SenderEmail == "" {
			switchCfg.SenderEmail = switchCfg.SMTPUsername
		}

		fmt.Println()
		fmt.Println("-- Recipients --")
		switchCfg.UserEmail = promptLine(reader, "Your email (for warnings): ")
		recipientStr := promptLine(reader, "Family recipient emails (comma-separated): ")
		for _, r := range strings.Split(recipientStr, ",") {
			r = strings.TrimSpace(r)
			if r != "" {
				switchCfg.Recipients = append(switchCfg.Recipients, r)
			}
		}
		if len(switchCfg.Recipients) == 0 {
			return fmt.Errorf("at least one recipient is required")
		}

		fmt.Println()
		fmt.Println("-- Escalation Thresholds --")
		fmt.Printf("Warning 1 (default: %d days): ", switchCfg.Warning1Days)
		if v := readLine(reader); v != "" {
			switchCfg.Warning1Days, _ = strconv.Atoi(v)
		}
		fmt.Printf("Warning 2 (default: %d days): ", switchCfg.Warning2Days)
		if v := readLine(reader); v != "" {
			switchCfg.Warning2Days, _ = strconv.Atoi(v)
		}
		fmt.Printf("Final release (default: %d days): ", switchCfg.FinalDays)
		if v := readLine(reader); v != "" {
			switchCfg.FinalDays, _ = strconv.Atoi(v)
		}

		// Telegram configuration
		fmt.Println()
		fmt.Println("-- Telegram Bot (optional) --")
		fmt.Println("A Telegram bot can ping you and accept /alive replies.")
		switchCfg.TelegramBotToken = promptLine(reader, "Telegram bot token (leave empty to skip): ")
		if switchCfg.TelegramBotToken != "" {
			fmt.Println("Send any message to your bot, then press Enter...")
			readLine(reader)
			chatID, err := deadswitch.ResolveChatID(switchCfg.TelegramBotToken)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not resolve chat ID: %v\n", err)
				switchCfg.TelegramChatID = promptLine(reader, "Enter chat ID manually: ")
			} else {
				fmt.Printf("Detected chat ID: %s\n", chatID)
				switchCfg.TelegramChatID = chatID
			}
			// Send test message
			if err := deadswitch.SendTelegramMessage(switchCfg.TelegramBotToken, switchCfg.TelegramChatID, "Kawarimi bot connected. Reply /alive to check in."); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: test message failed: %v\n", err)
			} else {
				fmt.Println("Test message sent!")
			}
		}

		// Ping channels
		fmt.Println()
		fmt.Println("-- Ping Channels --")
		fmt.Println("Which channels should be used to ping you?")
		channelStr := promptLine(reader, "Channels (comma-separated, options: email,telegram) [email]: ")
		if channelStr == "" {
			switchCfg.PingChannels = []string{"email"}
		} else {
			for _, c := range strings.Split(channelStr, ",") {
				c = strings.TrimSpace(strings.ToLower(c))
				if c == "email" || c == "telegram" {
					switchCfg.PingChannels = append(switchCfg.PingChannels, c)
				}
			}
		}

		// IMAP configuration
		fmt.Println()
		fmt.Println("-- Email Reply Check-in (optional) --")
		fmt.Println("If configured, you can check in by replying to ping emails with 'ALIVE'.")
		switchCfg.IMAPServer = promptLine(reader, "IMAP server (e.g., imap.gmail.com, leave empty to skip): ")
		if switchCfg.IMAPServer != "" {
			imapPortStr := promptLine(reader, "IMAP port (default: 993): ")
			if imapPortStr != "" {
				switchCfg.IMAPPort, _ = strconv.Atoi(imapPortStr)
			} else {
				switchCfg.IMAPPort = 993
			}
		}

		// Vault package location (for sealed payload mode)
		fmt.Println()
		fmt.Println("-- Vault Package Location --")
		fmt.Println("Where can recipients download the vault package?")
		fmt.Println("(e.g., a Google Drive link, GitHub release URL, or instructions)")
		switchCfg.VaultPackageLocation = promptLine(reader, "Vault package location: ")

		// Legacy delivery instructions (still useful as fallback)
		switchCfg.VaultRepoURL = promptLine(reader, "Vault git repo URL (optional, leave empty if using package): ")
		if switchCfg.VaultPackageLocation == "" {
			fmt.Println("Custom delivery instructions (free text, e.g., 'Contact John at 555-1234")
			fmt.Println("who has a USB copy' or 'Download from https://...')")
			switchCfg.DeliveryInstructions = promptLine(reader, "Instructions (leave empty for default): ")
		}

		// Store the switch payload
		fmt.Println()

		// Check for DMS key (V4, created during init)
		dmsKeyPath := filepath.Join(appDir, "dms-key")
		// Also check legacy sealed payload path for backward compat
		sealedPayloadPath := filepath.Join(appDir, "sealed-payload.age")

		if vault.IsV2Vault(cfg.VaultDir) {
			if _, err := os.Stat(dmsKeyPath); err == nil {
				// V4: DMS key mode
				dmsKeyBase64, err := os.ReadFile(dmsKeyPath)
				if err != nil {
					return fmt.Errorf("reading DMS key: %w", err)
				}

				if err := deadswitch.StoreSwitchDMSKey(appDir, strings.TrimSpace(string(dmsKeyBase64))); err != nil {
					return err
				}

				fmt.Println("DMS key loaded from init.")
				fmt.Println("The DMS will deliver this key to recipients when triggered.")
				fmt.Println("Recipients will also need the physical card with the recipient passphrase to decrypt.")
			} else if _, err := os.Stat(sealedPayloadPath); err == nil {
				// V3: Sealed payload mode (legacy)
				sealedPayload, err := os.ReadFile(sealedPayloadPath)
				if err != nil {
					return fmt.Errorf("reading sealed payload: %w", err)
				}

				sealedBase64 := crypto.EncodeSealedPayload(sealedPayload)
				if err := deadswitch.StoreSwitchSealedPayload(appDir, sealedBase64); err != nil {
					return err
				}

				fmt.Println("Sealed payload loaded from init (V3 legacy mode).")
				fmt.Println("The DMS will deliver this sealed payload to recipients when triggered.")
				fmt.Println("Recipients will need the physical card with the recipient passphrase to decrypt it.")
			} else {
				// Fallback: mnemonic mode (v2)
				fmt.Println("No DMS key or sealed payload found. Falling back to mnemonic mode.")
				fmt.Println("(Run 'kawarimi init' to generate a DMS key for the new architecture.)")
				fmt.Println()

				// Mnemonic delivery mode
				fmt.Println("-- Mnemonic Delivery --")
				fmt.Println("How should the 8 mnemonic words be delivered to the receiver?")
				fmt.Println("  1) email    - Include words directly in the notification email")
				fmt.Println("  2) physical - Reference a physical location (sealed envelope, safe)")
				modeStr := promptLine(reader, "Mode [physical]: ")
				if strings.HasPrefix(strings.ToLower(modeStr), "e") || modeStr == "1" {
					switchCfg.MnemonicDelivery = "email"
				} else {
					switchCfg.MnemonicDelivery = "physical"
				}

				// Physical location
				fmt.Println()
				fmt.Println("Where is the physical mnemonic backup stored?")
				switchCfg.PassphraseLocation = promptLine(reader, "Location (e.g., 'sealed envelope in home safe'): ")

				fmt.Println("Enter the 8 mnemonic words to store in the switch payload.")
				fmt.Println("These will be sent to recipients if the switch triggers.")
				fmt.Fprint(os.Stdout, "Enter 8 mnemonic words (space-separated): ")
				var words []string
				for i := 0; i < 8; i++ {
					var w string
					if _, err := fmt.Scan(&w); err != nil {
						return fmt.Errorf("reading mnemonic word %d: %w", i+1, err)
					}
					words = append(words, w)
				}

				// Verify mnemonic works
				header, err := vault.LoadHeader(cfg.VaultDir)
				if err != nil {
					return fmt.Errorf("loading vault header: %w", err)
				}
				mk, _, err := header.OpenWithMnemonic(words)
				if err != nil {
					return fmt.Errorf("invalid mnemonic: %w", err)
				}
				crypto.ZeroBytes(mk)

				if err := deadswitch.StoreSwitchMnemonic(appDir, words); err != nil {
					return err
				}
			}
		} else {
			fmt.Println("The vault passphrase is needed to set up the local (systemd) switch.")
			passphrase, err := crypto.PromptPassphrase("Enter vault passphrase: ")
			if err != nil {
				return err
			}

			_, err = openVaultWithPassphrase(cfg.VaultDir, passphrase)
			if err != nil {
				return fmt.Errorf("invalid passphrase: %w", err)
			}

			if err := deadswitch.StoreSwitchPayload(appDir, passphrase); err != nil {
				return err
			}
		}
		if err := deadswitch.SaveSwitchConfig(appDir, switchCfg); err != nil {
			return err
		}

		// Install GitHub Actions workflow
		fmt.Println()
		useDMSKeyMode := false
		if _, err := os.Stat(dmsKeyPath); err == nil {
			useDMSKeyMode = true
		}
		useSealedMode := false
		if !useDMSKeyMode {
			if _, err := os.Stat(sealedPayloadPath); err == nil {
				useSealedMode = true
			}
		}

		if useDMSKeyMode {
			// V4: arm the standalone cloud DMS repo (workflow + seeded heartbeat + push).
			fmt.Println("-- Arming the cloud dead man's switch --")
			if err := runSwitchSeed(reader, cfg, switchCfg, false); err != nil {
				fmt.Fprintf(os.Stderr, "\nCould not arm the cloud switch yet: %v\n", err)
				fmt.Println("Create the empty DMS repo, then run 'kawarimi switch seed' to finish.")
			}
		} else if useSealedMode {
			// V3 legacy: Generate DMS workflow with sealed payload
			dmsOutputDir := filepath.Join(appDir, "dms-workflow")
			if err := deadswitch.GenerateGitHubDMSWorkflowFile(dmsOutputDir, switchCfg); err != nil {
				return err
			}
			fmt.Printf("GitHub Actions DMS workflow generated at:\n  %s\n", filepath.Join(dmsOutputDir, ".github", "workflows", "deadman.yml"))
			fmt.Println()
			fmt.Println("Create a SEPARATE GitHub repo for the DMS (not the vault storage repo!).")
			fmt.Println("Copy the workflow file and configure these repo secrets:")
			fmt.Println("  SMTP_SERVER, SMTP_USERNAME, SMTP_PASSWORD")
			fmt.Println("  USER_EMAIL, RECIPIENT_EMAILS")
			fmt.Println("  SEALED_PAYLOAD (the base64 sealed payload)")
			fmt.Println("  VAULT_PACKAGE_LOCATION (where recipients download the vault)")
			if switchCfg.TelegramBotToken != "" {
				fmt.Println("  TELEGRAM_BOT_TOKEN, TELEGRAM_CHAT_ID")
			}
			fmt.Println()
			fmt.Println("The sealed payload value to set as a secret:")
			sealedPayload, _ := os.ReadFile(sealedPayloadPath)
			fmt.Printf("  %s\n", crypto.EncodeSealedPayload(sealedPayload))
		} else {
			// Legacy: install workflow in vault repo
			fmt.Println("Installing GitHub Actions workflow...")
			if err := deadswitch.InstallGitHubWorkflow(cfg.VaultDir, switchCfg); err != nil {
				return err
			}
			fmt.Println("Created .github/workflows/deadman.yml in vault directory.")
			fmt.Println()
			fmt.Println("IMPORTANT: Configure these GitHub repo secrets:")
			fmt.Println("  SMTP_SERVER, SMTP_USERNAME, SMTP_PASSWORD")
			fmt.Println("  USER_EMAIL, RECIPIENT_EMAILS")
			fmt.Println("  PHYSICAL_PASSPHRASE_LOCATION")
			if switchCfg.TelegramBotToken != "" {
				fmt.Println("  TELEGRAM_BOT_TOKEN, TELEGRAM_CHAT_ID")
			}
		}

		// Install systemd timer
		fmt.Println()
		fmt.Println("Installing systemd timer...")
		binary, err := os.Executable()
		if err != nil {
			binary = "kawarimi"
		}
		if err := deadswitch.InstallSystemdUnits(binary); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not install systemd units: %v\n", err)
		} else {
			fmt.Println("Systemd units installed.")
			fmt.Println("Enable with: systemctl --user enable --now kawarimi-switch.timer")
		}

		fmt.Println()
		fmt.Println("Switch setup complete.")
		fmt.Println("Run 'kawarimi checkin' to record your first check-in.")
		fmt.Println("Run 'kawarimi switch test' to send a test email.")

		return nil
	},
}

var switchTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Send a test notification",
	RunE: func(cmd *cobra.Command, args []string) error {
		appDir, err := config.AppDirPath()
		if err != nil {
			return err
		}

		switchCfg, err := deadswitch.LoadSwitchConfig(appDir)
		if err != nil {
			return fmt.Errorf("loading switch config: %w", err)
		}

		fmt.Println("Sending test email to your address...")
		if err := deadswitch.SendEmail(switchCfg, []string{switchCfg.UserEmail},
			"Kawarimi: Test notification",
			"This is a test notification from Kawarimi's dead man's switch.\n\nIf you received this, email delivery is working correctly."); err != nil {
			return fmt.Errorf("test email failed: %w", err)
		}
		fmt.Printf("Test email sent to %s\n", switchCfg.UserEmail)

		return nil
	},
}

var switchDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable the dead man's switch",
	RunE: func(cmd *cobra.Command, args []string) error {
		appDir, err := config.AppDirPath()
		if err != nil {
			return err
		}

		// Remove switch files
		files := []string{"switch-identity.key", "switch-payload.age", "switch-config.age"}
		for _, f := range files {
			os.Remove(filepath.Join(appDir, f))
		}

		// Try to disable systemd timer
		exec.Command("systemctl", "--user", "disable", "--now", "kawarimi-switch.timer").Run()

		fmt.Println("Dead man's switch disabled.")
		fmt.Println("The GitHub Actions workflow file remains in the vault — remove it manually if needed.")

		return nil
	},
}

var switchSeedCmd = &cobra.Command{
	Use:   "seed",
	Short: "Arm or repair the cloud dead man's switch",
	Long: `Pushes the dead man's switch workflow and a fresh heartbeat (last_checkin) to
the standalone DMS GitHub repo, so the switch actually reads your check-ins.

Run this once after 'kawarimi switch setup', and again any time you change
switch settings or need to repair the repo (use --force if it has diverged).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		appDir, err := config.AppDirPath()
		if err != nil {
			return err
		}
		if !deadswitch.IsSwitchConfigured(appDir) {
			return fmt.Errorf("switch not configured — run 'kawarimi switch setup' first")
		}
		switchCfg, err := deadswitch.LoadSwitchConfig(appDir)
		if err != nil {
			return err
		}
		reader := bufio.NewReader(os.Stdin)
		return runSwitchSeed(reader, cfg, switchCfg, switchSeedForce)
	},
}

// runSwitchSeed writes the workflow + a seeded heartbeat + README into the local
// DMS repo clone and pushes them to the configured DMS remote. It is idempotent:
// safe to run repeatedly to arm or repair the switch. reader is shared with the
// caller so it can prompt for the DMS remote without a second os.Stdin buffer.
func runSwitchSeed(reader *bufio.Reader, cfg *config.Config, switchCfg *deadswitch.SwitchConfig, force bool) error {
	if cfg.SyncTargets.DMSRemote == "" {
		fmt.Println("The dead man's switch needs its OWN GitHub repo, separate from the vault repo.")
		fmt.Println("Create a new PRIVATE, EMPTY repo (no README, no .gitignore), then paste its SSH URL.")
		remote := promptLine(reader, "DMS repo SSH URL (git@github.com:you/dms.git): ")
		if remote == "" {
			return fmt.Errorf("a DMS repo SSH URL is required to arm the cloud switch")
		}
		cfg.SyncTargets.DMSRemote = remote
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}
	}

	dmsRepoDir, err := config.DMSRepoDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dmsRepoDir, 0700); err != nil {
		return fmt.Errorf("creating DMS repo dir: %w", err)
	}

	gs := gosync.NewGitSync(dmsRepoDir, cfg.SyncTargets.DMSRemote, "")
	// Build on top of whatever is already on the remote (best effort).
	if err := gs.ResetToRemote(); err != nil {
		return fmt.Errorf("syncing DMS repo from remote: %w", err)
	}

	if err := deadswitch.GenerateGitHubDMSWorkflowFile(dmsRepoDir, switchCfg); err != nil {
		return err
	}
	stamp := time.Now().UTC().Format(time.RFC3339)
	if err := os.WriteFile(filepath.Join(dmsRepoDir, "last_checkin"), []byte(stamp+"\n"), 0644); err != nil {
		return fmt.Errorf("writing heartbeat: %w", err)
	}
	readme := "# Kawarimi dead man's switch\n\nHeartbeat repo — do not delete.\n`last_checkin` is updated automatically by `kawarimi checkin`.\n"
	if err := os.WriteFile(filepath.Join(dmsRepoDir, "README.md"), []byte(readme), 0644); err != nil {
		return fmt.Errorf("writing README: %w", err)
	}

	if force {
		if _, err := gs.Commit("seed dead man's switch " + stamp); err != nil {
			return err
		}
		if err := gs.ForcePush(); err != nil {
			return fmt.Errorf("force pushing DMS repo: %w", err)
		}
	} else {
		if err := gs.CommitAndPush("seed dead man's switch " + stamp); err != nil {
			return fmt.Errorf("pushing DMS repo (if it already has commits, retry with --force): %w", err)
		}
	}

	fmt.Println()
	fmt.Println("Cloud dead man's switch armed.")
	fmt.Printf("  DMS repo:    %s\n", cfg.SyncTargets.DMSRemote)
	fmt.Printf("  Local clone: %s\n", dmsRepoDir)
	fmt.Println()
	fmt.Println("In the DMS GitHub repo (Settings -> Secrets and variables -> Actions), set:")
	fmt.Println("  SMTP_SERVER, SMTP_USERNAME, SMTP_PASSWORD")
	fmt.Println("  USER_EMAIL, RECIPIENT_EMAILS")
	fmt.Println("  DMS_KEY, VAULT_PACKAGE_LOCATION")
	if switchCfg.TelegramBotToken != "" {
		fmt.Println("  TELEGRAM_BOT_TOKEN, TELEGRAM_CHAT_ID")
	}

	appDir, err := config.AppDirPath()
	if err == nil {
		if data, rerr := os.ReadFile(filepath.Join(appDir, "dms-key")); rerr == nil {
			fmt.Println()
			fmt.Println("DMS_KEY value to set as the secret above:")
			fmt.Printf("  %s\n", strings.TrimSpace(string(data)))
		}
	}

	fmt.Println()
	fmt.Println("Enable the GitHub Actions workflow in the repo, then confirm with:")
	fmt.Println("  kawarimi switch verify")
	return nil
}

var switchEvaluateCmd = &cobra.Command{
	Use:    "evaluate",
	Short:  "Evaluate the switch (called by systemd timer)",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		appDir, err := config.AppDirPath()
		if err != nil {
			return err
		}

		if !deadswitch.IsSwitchConfigured(appDir) {
			return fmt.Errorf("switch not configured — run 'kawarimi switch setup'")
		}

		switchCfg, err := deadswitch.LoadSwitchConfig(appDir)
		if err != nil {
			return err
		}

		targets, err := checkinTargets(cfg)
		if err != nil {
			return err
		}

		return deadswitch.Evaluate(targets, switchCfg, appDir)
	},
}

// checkinTargets builds the check-in destinations from config: always the local
// vault, plus the DMS heartbeat repo when a DMS remote is configured.
func checkinTargets(cfg *config.Config) (deadswitch.CheckinTargets, error) {
	dmsRepoDir, err := config.DMSRepoDir()
	if err != nil {
		return deadswitch.CheckinTargets{}, err
	}
	return deadswitch.CheckinTargets{
		VaultDir:   cfg.VaultDir,
		DMSRepoDir: dmsRepoDir,
		DMSRemote:  cfg.SyncTargets.DMSRemote,
	}, nil
}

func readLine(reader *bufio.Reader) string {
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(line)
}

func openVaultWithPassphrase(vaultDir, passphrase string) (bool, error) {
	// Actually verify the passphrase by decrypting the manifest
	_, err := vault.Open(vaultDir, passphrase)
	if err != nil {
		return false, err
	}
	return true, nil
}
