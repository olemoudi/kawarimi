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

var (
	switchSeedForce             bool
	switchRekeyRotatePassphrase bool
)

func init() {
	rootCmd.AddCommand(switchCmd)
	switchCmd.AddCommand(switchSetupCmd)
	switchCmd.AddCommand(switchSeedCmd)
	switchCmd.AddCommand(switchVerifyCmd)
	switchCmd.AddCommand(switchRekeyCmd)
	switchCmd.AddCommand(switchTestCmd)
	switchCmd.AddCommand(switchDisableCmd)
	switchCmd.AddCommand(switchEvaluateCmd)

	switchSeedCmd.Flags().BoolVar(&switchSeedForce, "force", false, "force-push the DMS repo (overwrites remote history; use to repair a diverged repo)")
	switchRekeyCmd.Flags().BoolVar(&switchRekeyRotatePassphrase, "rotate-passphrase", false, "also generate a new recipient passphrase (requires re-printing and re-distributing the cards)")
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

		if vault.IsV2Vault(cfg.VaultDir) {
			// V4 is the only supported architecture for V2 vaults. init writes the
			// DMS key; if it is missing (e.g. a vault created before V4, or one whose
			// key was rotated away), rekey regenerates it.
			if _, err := os.Stat(dmsKeyPath); err != nil {
				return fmt.Errorf("no DMS key found — run 'kawarimi switch rekey' first to generate V4 switch material")
			}
			dmsKeyBase64, err := os.ReadFile(dmsKeyPath)
			if err != nil {
				return fmt.Errorf("reading DMS key: %w", err)
			}
			if err := deadswitch.StoreSwitchDMSKey(appDir, strings.TrimSpace(string(dmsKeyBase64))); err != nil {
				return err
			}
			fmt.Println("DMS key loaded.")
			fmt.Println("The DMS delivers this key to recipients when triggered; they also need the")
			fmt.Println("physical card with the recipient passphrase to decrypt.")
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
		if _, err := os.Stat(dmsKeyPath); err == nil {
			// V4: arm the standalone cloud DMS repo (workflow + seeded heartbeat + push).
			fmt.Println("-- Arming the cloud dead man's switch --")
			if err := runSwitchSeed(reader, cfg, switchCfg, false); err != nil {
				fmt.Fprintf(os.Stderr, "\nCould not arm the cloud switch yet: %v\n", err)
				fmt.Println("Create the empty DMS repo, then run 'kawarimi switch seed' to finish.")
			}
		} else {
			// Legacy v1 (headerless) vault: install the passphrase-location workflow
			// in the vault repo itself.
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

var switchRekeyCmd = &cobra.Command{
	Use:   "rekey",
	Short: "Rotate the DMS key after a false trigger or key leak",
	Long: `Generates a new DMS key and re-seals the vault payload with it. Use this if the
switch fired by mistake and the DMS key reached someone other than your intended
recipients, or if the key leaked.

By default the recipient passphrase (the physical card) is unchanged, so cards you
have already handed out stay valid. Pass --rotate-passphrase to also generate a new
one (you must then re-print and re-distribute the cards).

Requires your 8 mnemonic words (paper backup) to re-seal; they are not stored.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		appDir, err := config.AppDirPath()
		if err != nil {
			return err
		}

		header, err := vault.LoadHeader(cfg.VaultDir)
		if err != nil {
			return fmt.Errorf("loading vault header (rekey needs a V2 vault): %w", err)
		}

		reader := bufio.NewReader(os.Stdin)
		fmt.Println("Rekey needs your 8 mnemonic words (from your paper backup).")
		fmt.Println("They re-seal the vault and are not stored anywhere.")
		words := make([]string, 8)
		for i := range words {
			words[i] = promptLine(reader, fmt.Sprintf("Word %d: ", i+1))
		}

		masterKey, _, err := header.OpenWithMnemonic(words)
		if err != nil {
			return fmt.Errorf("those words did not unlock the vault: %w", err)
		}
		crypto.ZeroBytes(masterKey)

		entropy, err := crypto.DecodeMnemonic(words)
		if err != nil {
			return fmt.Errorf("decoding mnemonic: %w", err)
		}
		defer crypto.ZeroBytes(entropy)

		var passphrase string
		if switchRekeyRotatePassphrase {
			passphrase, err = crypto.GenerateRecipientPassphrase()
			if err != nil {
				return fmt.Errorf("generating recipient passphrase: %w", err)
			}
		} else {
			passphrase = promptLine(reader, "Recipient passphrase from the physical card (unchanged): ")
			if passphrase == "" {
				return fmt.Errorf("recipient passphrase required (or pass --rotate-passphrase to make a new one)")
			}
		}

		dmsKeyB64, err := sealAndInstallV4(cfg.VaultDir, appDir, entropy, passphrase)
		if err != nil {
			return err
		}

		// Update the stored (encrypted) switch payload so the local systemd path
		// also delivers the new key.
		if deadswitch.IsSwitchConfigured(appDir) {
			if err := deadswitch.StoreSwitchDMSKey(appDir, dmsKeyB64); err != nil {
				return fmt.Errorf("updating stored switch payload: %w", err)
			}
		}

		// If the switch had triggered, offer to clear the marker and re-arm.
		triggeredPath := filepath.Join(appDir, "switch-triggered")
		if _, err := os.Stat(triggeredPath); err == nil {
			ans := promptLine(reader, "The switch is marked TRIGGERED. Clear it and re-arm? [y/N]: ")
			if strings.HasPrefix(strings.ToLower(ans), "y") {
				os.Remove(triggeredPath)
				if targets, terr := checkinTargets(cfg); terr == nil {
					if _, cerr := deadswitch.RecordCheckin(targets, time.Now()); cerr != nil {
						fmt.Fprintf(os.Stderr, "Warning: re-arm check-in did not reach the cloud DMS: %v\n", cerr)
					}
				}
			}
		}

		fmt.Println()
		fmt.Println("========================================")
		fmt.Println("  DMS KEY ROTATED")
		fmt.Println("========================================")
		if switchRekeyRotatePassphrase {
			fmt.Println()
			fmt.Println("NEW RECIPIENT PASSPHRASE (re-print the card and re-distribute to recipients):")
			fmt.Printf("  %s\n", passphrase)
		}
		fmt.Println()
		fmt.Println("New DMS_KEY value:")
		fmt.Printf("  %s\n", dmsKeyB64)
		fmt.Println()
		fmt.Println("Finish the rotation:")
		fmt.Println("  1. Update the GitHub secret DMS_KEY in your DMS repo to the value above")
		fmt.Println("     (or run 'kawarimi switch seed' to reprint the checklist).")
		fmt.Println("  2. Run 'kawarimi package build' and re-upload it to VAULT_PACKAGE_LOCATION.")
		fmt.Println("  3. Replace or destroy old package copies (USB, cloud) — they carry the old seal.")
		fmt.Println("  4. Run 'kawarimi switch verify'.")
		return nil
	},
}

// printTriggeredWarning explains a post-trigger situation appropriately for the
// vault's architecture: V4 vaults disclosed a DMS key (which alone cannot open the
// vault), whereas legacy v1 vaults disclosed the passphrase itself.
func printTriggeredWarning(vaultDir string) {
	if _, err := os.Stat(filepath.Join(vaultDir, vault.SealedPayloadFile)); err == nil {
		fmt.Println("WARNING: the dead man's switch has TRIGGERED.")
		fmt.Println("The DMS key may have been disclosed to whoever received the release email.")
		fmt.Println("If that reached anyone beyond your intended recipients, run 'kawarimi switch rekey'.")
		fmt.Println("(The DMS key alone cannot open the vault — the recipient card passphrase is also required.)")
		return
	}
	fmt.Println("WARNING: the dead man's switch has TRIGGERED.")
	fmt.Println("Your passphrase may have been sent to recipients.")
	fmt.Println("Run 'kawarimi passwd' to change your passphrase.")
}

var switchVerifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Check that the dead man's switch is armed and current",
	Long: `Verifies the switch end-to-end: that a local check-in exists, and that the
cloud DMS repo has a current heartbeat and an up-to-date workflow. This is what
catches a switch that silently stopped working (e.g. a stale workflow, or
check-ins that never reach the repo).`,
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
		targets, err := checkinTargets(cfg)
		if err != nil {
			return err
		}

		fmt.Println("Checking dead man's switch...")
		report, err := deadswitch.Verify(targets, switchCfg, appDir)
		if err != nil {
			return err
		}
		printVerifyReport(report, switchCfg)

		if !report.OK() {
			return fmt.Errorf("verification FAILED — fix the items above (usually 'kawarimi switch seed')")
		}
		return nil
	},
}

func printVerifyReport(r *deadswitch.VerifyReport, switchCfg *deadswitch.SwitchConfig) {
	mark := func(ok bool) string {
		if ok {
			return "[PASS]"
		}
		return "[FAIL]"
	}
	const warn = "[WARN]"

	fmt.Println()

	if r.LocalCheckinErr != nil {
		fmt.Printf("%s Local check-in: none recorded (run 'kawarimi checkin')\n", mark(false))
	} else {
		fmt.Printf("%s Local check-in: %s\n", mark(true), r.LocalCheckin.Format(time.RFC3339))
	}

	if !r.DMSConfigured {
		fmt.Printf("%s Cloud DMS: not configured (run 'kawarimi switch seed' to arm it)\n", warn)
	} else {
		fmt.Printf("       Cloud DMS repo: %s\n", r.DMSRemote)

		switch {
		case r.RemoteCheckinErr != nil:
			fmt.Printf("%s Remote heartbeat: unreadable (%v)\n", mark(false), r.RemoteCheckinErr)
		case r.RemoteStale:
			fmt.Printf("%s Remote heartbeat: STALE %s — local is newer, check-ins are not reaching the repo\n",
				mark(false), r.RemoteCheckin.Format(time.RFC3339))
		default:
			fmt.Printf("%s Remote heartbeat: %s\n", mark(true), r.RemoteCheckin.Format(time.RFC3339))
		}

		switch {
		case !r.WorkflowPresent:
			fmt.Printf("%s Workflow: missing on remote (run 'kawarimi switch seed')\n", mark(false))
		case !r.WorkflowUpToDate:
			fmt.Printf("%s Workflow: OUT OF DATE (run 'kawarimi switch seed' to update it)\n", mark(false))
		default:
			fmt.Printf("%s Workflow: up to date\n", mark(true))
		}
	}

	if r.SystemdTimerActive {
		fmt.Println("       Local systemd timer: active")
	} else {
		fmt.Println("       Local systemd timer: inactive (the cloud DMS is the real post-mortem trigger)")
	}

	if r.FinalDaysRisky {
		fmt.Printf("%s FinalDays=%d risks GitHub auto-disabling the scheduled workflow (it disables\n", warn, switchCfg.FinalDays)
		fmt.Println("       schedules after ~60 days of repo inactivity). Consider a lower FinalDays.")
	}

	if r.Triggered {
		fmt.Printf("%s Switch has already TRIGGERED — the DMS key may have been disclosed.\n", warn)
	}

	if r.LegacyMnemonicEmail {
		fmt.Printf("%s Legacy payload emails the mnemonic words outright — anyone reading that inbox\n", warn)
		fmt.Println("       could open the vault. Run 'kawarimi switch rekey' to move to V4.")
	}

	fmt.Println()
	fmt.Println("Not checkable from here: the GitHub repo secrets (DMS_KEY, SMTP_*, RECIPIENT_EMAILS,")
	fmt.Println("VAULT_PACKAGE_LOCATION) and real email delivery. Confirm those in the repo settings")
	fmt.Println("and with 'kawarimi switch test'.")
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
