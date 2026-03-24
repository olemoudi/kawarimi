package deadswitch

import (
	"fmt"
	"os"
	"path/filepath"
)

// GenerateGitHubWorkflow returns the GitHub Actions workflow YAML for the dead man's switch.
func GenerateGitHubWorkflow(cfg *SwitchConfig) string {
	return fmt.Sprintf(`name: Dead Man's Switch
on:
  schedule:
    - cron: '0 9 * * *'  # Daily at 9am UTC
  workflow_dispatch: {}   # Allow manual trigger

jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Check last check-in
        id: checkin
        run: |
          if [ ! -f last_checkin ]; then
            echo "days_since=999" >> $GITHUB_OUTPUT
            exit 0
          fi
          last=$(cat last_checkin | tr -d '[:space:]')
          last_epoch=$(date -d "$last" +%%s 2>/dev/null || date -j -f "%%Y-%%m-%%dT%%H:%%M:%%SZ" "$last" +%%s 2>/dev/null)
          now_epoch=$(date +%%s)
          diff=$(( (now_epoch - last_epoch) / 86400 ))
          echo "days_since=$diff" >> $GITHUB_OUTPUT
          echo "Last check-in: $last ($diff days ago)"

      - name: Send warning to owner
        if: steps.checkin.outputs.days_since >= %d && steps.checkin.outputs.days_since < %d
        uses: dawidd6/action-send-mail@v3
        with:
          server_address: ${{ secrets.SMTP_SERVER }}
          server_port: 587
          username: ${{ secrets.SMTP_USERNAME }}
          password: ${{ secrets.SMTP_PASSWORD }}
          subject: "Kawarimi: Check-in overdue (${{ steps.checkin.outputs.days_since }} days)"
          to: ${{ secrets.USER_EMAIL }}
          from: ${{ secrets.SMTP_USERNAME }}
          body: |
            You haven't checked in for ${{ steps.checkin.outputs.days_since }} days.

            Run 'kawarimi checkin' to reset the timer.

            If you don't check in by day %d, your family will be notified.

      - name: Notify family
        if: steps.checkin.outputs.days_since >= %d
        uses: dawidd6/action-send-mail@v3
        with:
          server_address: ${{ secrets.SMTP_SERVER }}
          server_port: 587
          username: ${{ secrets.SMTP_USERNAME }}
          password: ${{ secrets.SMTP_PASSWORD }}
          subject: "Important: Information Vault Access"
          to: ${{ secrets.RECIPIENT_EMAILS }}
          from: ${{ secrets.SMTP_USERNAME }}
          body: |
            This is an automated message.

            The vault owner has not checked in for ${{ steps.checkin.outputs.days_since }} days.

            To access the encrypted information vault:

            1. The vault is in this repository. Clone or download it.

            2. The decryption passphrase is located at:
               ${{ secrets.PHYSICAL_PASSPHRASE_LOCATION }}

            3. Read the DECRYPT_INSTRUCTIONS.md file in this repository for
               step-by-step decryption instructions.

            4. You will need the 'age' tool: https://github.com/FiloSottile/age/releases
`, cfg.Warning1Days, cfg.FinalDays, cfg.FinalDays, cfg.FinalDays)
}

// InstallGitHubWorkflow writes the workflow file to the vault's .github/workflows/ directory.
func InstallGitHubWorkflow(vaultDir string, cfg *SwitchConfig) error {
	workflowDir := filepath.Join(vaultDir, ".github", "workflows")
	if err := os.MkdirAll(workflowDir, 0755); err != nil {
		return fmt.Errorf("creating workflow dir: %w", err)
	}

	content := GenerateGitHubWorkflow(cfg)
	path := filepath.Join(workflowDir, "deadman.yml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("writing workflow: %w", err)
	}

	return nil
}
