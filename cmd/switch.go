package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/olemoudi/kawarimi/internal/config"
	"github.com/olemoudi/kawarimi/internal/crypto"
	"github.com/olemoudi/kawarimi/internal/deadswitch"
	"github.com/olemoudi/kawarimi/internal/vault"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(switchCmd)
	switchCmd.AddCommand(switchSetupCmd)
	switchCmd.AddCommand(switchTestCmd)
	switchCmd.AddCommand(switchDisableCmd)
	switchCmd.AddCommand(switchEvaluateCmd)
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

		fmt.Println()
		fmt.Println("-- Physical Passphrase Location --")
		fmt.Println("Where is the physical passphrase backup stored?")
		fmt.Println("(This text will be included in the GitHub Actions notification email)")
		switchCfg.PassphraseLocation = promptLine(reader, "Location: ")

		switchCfg.VaultRepoURL = promptLine(reader, "Vault git repo URL (for notification emails): ")

		// Store the vault passphrase for the systemd switch
		fmt.Println()
		fmt.Println("The vault passphrase is needed to set up the local (systemd) switch.")
		passphrase, err := crypto.PromptPassphrase("Enter vault passphrase: ")
		if err != nil {
			return err
		}

		// Verify passphrase works by opening the vault
		_, err = openVaultWithPassphrase(cfg.VaultDir, passphrase)
		if err != nil {
			return fmt.Errorf("invalid passphrase: %w", err)
		}

		// Store switch payload and config
		if err := deadswitch.StoreSwitchPayload(appDir, passphrase); err != nil {
			return err
		}
		if err := deadswitch.SaveSwitchConfig(appDir, switchCfg); err != nil {
			return err
		}

		// Install GitHub Actions workflow
		fmt.Println()
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

		return deadswitch.Evaluate(cfg.VaultDir, switchCfg, appDir)
	},
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
