package deadswitch

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/olemoudi/kawarimi/internal/copytext"
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

// dmsWorkflowParams are the fields substituted into dmsWorkflowText.
// DMSWorkflowVersion is the generation of the generated cloud DMS workflow. Bump it
// whenever the workflow's logic changes materially (a new mailer, a security fix, a
// step change) so a deployed switch running an older automation is detected as
// outdated and the owner is told to re-run `kawarimi switch seed`. History:
// v1 = the original dawidd6/action-send-mail workflow; v2 = the self-contained curl
// mailer (no third-party action, templated SMTP port/scheme); v3 = release email
// generated from copytext (one canonical body with the local path; the physical
// card is now explained before the steps).
const DMSWorkflowVersion = 3

type dmsWorkflowParams struct {
	WorkflowVersion int
	Warning1Days    int
	FinalDays       int
	SMTPPort        int    // templated into the workflow (was hardcoded 587)
	Scheme          string // "smtp" (STARTTLS) or "smtps" (implicit TLS on 465)
	Telegram        bool   // include a Telegram owner-alert step
	ReleaseBody     string // recipient email body, pre-indented to the run-block column
}

// dmsWorkflowTmpl renders the standalone DMS repo workflow. It uses [[ ]] delimiters
// so that GitHub Actions ${{ }} expressions pass through untouched (no escaping), which
// is what makes the previous fmt.Sprintf %-escaping bug class impossible to reintroduce.
var dmsWorkflowTmpl = template.Must(
	template.New("dms").Delims("[[", "]]").Parse(dmsWorkflowText),
)

// GenerateGitHubDMSWorkflow returns a GitHub Actions workflow YAML for a standalone DMS repo.
// V4: This workflow delivers the DMS key. The sealed payload is in the vault package itself.
// Recipients combine the DMS key with their recipient passphrase to decrypt.
//
// Fail semantics: if last_checkin is missing or unparseable the job alerts the OWNER and
// never releases (fail-closed toward disclosure, fail-open toward owner alerting). The DMS
// key is delivered only when a check-in was read successfully AND is older than FinalDays.
func GenerateGitHubDMSWorkflow(cfg *SwitchConfig) string {
	port := cfg.SMTPPort
	if port == 0 {
		port = 587
	}
	scheme := "smtp" // STARTTLS (587 and most submission ports)
	if port == 465 {
		scheme = "smtps" // implicit TLS
	}
	params := dmsWorkflowParams{
		WorkflowVersion: DMSWorkflowVersion,
		Warning1Days:    cfg.Warning1Days,
		FinalDays:       cfg.FinalDays,
		SMTPPort:        port,
		Scheme:          scheme,
		Telegram:        cfg.TelegramBotToken != "",
		ReleaseBody:     indentForRunBlock(copytext.ReleaseEmailBodyWorkflow()),
	}
	var buf bytes.Buffer
	if err := dmsWorkflowTmpl.Execute(&buf, params); err != nil {
		// The template is a compile-time constant with known fields; execution cannot
		// fail at runtime. Panicking here surfaces any future template edit mistake in tests.
		panic(fmt.Sprintf("rendering DMS workflow: %v", err))
	}
	return buf.String()
}

// dmsWorkflowText is the standalone DMS repo workflow. Email is sent with curl (a
// tool preinstalled on GitHub runners and among the most stable in existence) rather
// than a third-party Action, so nothing beyond GitHub's own actions/checkout can
// vanish or be deprecated out from under it over the years it must keep working.
const dmsWorkflowText = `# kawarimi-dms-workflow-version: [[.WorkflowVersion]]
name: Dead Man's Switch
on:
  schedule:
    - cron: '0 9 * * *'  # Daily at 9am UTC
  workflow_dispatch: {}   # Allow manual trigger

permissions:
  contents: read

jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683  # v4.2.2

      - name: Check last check-in
        id: checkin
        run: |
          if [ ! -f last_checkin ]; then
            echo "status=missing" >> "$GITHUB_OUTPUT"
            echo "days_since=-1" >> "$GITHUB_OUTPUT"
            exit 0
          fi
          last=$(tr -d '[:space:]' < last_checkin)
          last_epoch=$(date -d "$last" +%s 2>/dev/null || true)
          if [ -z "$last_epoch" ]; then
            echo "status=unparseable" >> "$GITHUB_OUTPUT"
            echo "days_since=-1" >> "$GITHUB_OUTPUT"
            exit 0
          fi
          now_epoch=$(date +%s)
          diff=$(( (now_epoch - last_epoch) / 86400 ))
          echo "status=ok" >> "$GITHUB_OUTPUT"
          echo "days_since=$diff" >> "$GITHUB_OUTPUT"
          echo "Last check-in: $last ($diff days ago)"

      - name: Alert owner of DMS misconfiguration
        if: steps.checkin.outputs.status != 'ok'
        env:
          SMTP_SERVER: ${{ secrets.SMTP_SERVER }}
          SMTP_USERNAME: ${{ secrets.SMTP_USERNAME }}
          SMTP_PASSWORD: ${{ secrets.SMTP_PASSWORD }}
          USER_EMAIL: ${{ secrets.USER_EMAIL }}
          STATUS: ${{ steps.checkin.outputs.status }}
        run: |
          set -euo pipefail
          cat > message.txt <<EOF
          From: $SMTP_USERNAME
          To: $USER_EMAIL
          Subject: Kawarimi: DEAD MAN'S SWITCH IS NOT ARMED
          MIME-Version: 1.0
          Content-Type: text/plain; charset=UTF-8

          Your Kawarimi dead man's switch is NOT armed.

          The heartbeat file 'last_checkin' is $STATUS in the DMS repository, so the
          switch can never trigger and your recipients would never be notified.

          Fix it by running:  kawarimi switch seed

          This message repeats daily until the switch is armed.
          EOF
          sed 's/$/\r/' message.txt > message.eml
          curl --silent --show-error --ssl-reqd \
            --url "[[.Scheme]]://$SMTP_SERVER:[[.SMTPPort]]" \
            --user "$SMTP_USERNAME:$SMTP_PASSWORD" \
            --mail-from "$SMTP_USERNAME" \
            --mail-rcpt "$USER_EMAIL" \
            --upload-file message.eml

      - name: Send warning to owner
        if: steps.checkin.outputs.status == 'ok' && steps.checkin.outputs.days_since >= [[.Warning1Days]] && steps.checkin.outputs.days_since < [[.FinalDays]]
        env:
          SMTP_SERVER: ${{ secrets.SMTP_SERVER }}
          SMTP_USERNAME: ${{ secrets.SMTP_USERNAME }}
          SMTP_PASSWORD: ${{ secrets.SMTP_PASSWORD }}
          USER_EMAIL: ${{ secrets.USER_EMAIL }}
          DAYS: ${{ steps.checkin.outputs.days_since }}
        run: |
          set -euo pipefail
          cat > message.txt <<EOF
          From: $SMTP_USERNAME
          To: $USER_EMAIL
          Subject: Kawarimi: Check-in overdue ($DAYS days)
          MIME-Version: 1.0
          Content-Type: text/plain; charset=UTF-8

          You haven't checked in for $DAYS days.

          Run 'kawarimi checkin' to reset the timer.

          If you don't check in by day [[.FinalDays]], your family will be notified.
          EOF
          sed 's/$/\r/' message.txt > message.eml
          curl --silent --show-error --ssl-reqd \
            --url "[[.Scheme]]://$SMTP_SERVER:[[.SMTPPort]]" \
            --user "$SMTP_USERNAME:$SMTP_PASSWORD" \
            --mail-from "$SMTP_USERNAME" \
            --mail-rcpt "$USER_EMAIL" \
            --upload-file message.eml
[[if .Telegram]]
      - name: Telegram alert to owner
        if: steps.checkin.outputs.status != 'ok' || (steps.checkin.outputs.days_since >= [[.Warning1Days]] && steps.checkin.outputs.days_since < [[.FinalDays]])
        env:
          TG_TOKEN: ${{ secrets.TELEGRAM_BOT_TOKEN }}
          TG_CHAT: ${{ secrets.TELEGRAM_CHAT_ID }}
          TG_STATUS: ${{ steps.checkin.outputs.status }}
          TG_DAYS: ${{ steps.checkin.outputs.days_since }}
        run: |
          curl -sS --fail "https://api.telegram.org/bot${TG_TOKEN}/sendMessage" \
            --data-urlencode chat_id="${TG_CHAT}" \
            --data-urlencode "text=Kawarimi DMS needs attention (status=${TG_STATUS}, days=${TG_DAYS}). Run 'kawarimi checkin'."
[[end]]
      - name: Deliver DMS key to recipients
        if: steps.checkin.outputs.status == 'ok' && steps.checkin.outputs.days_since >= [[.FinalDays]]
        env:
          SMTP_SERVER: ${{ secrets.SMTP_SERVER }}
          SMTP_USERNAME: ${{ secrets.SMTP_USERNAME }}
          SMTP_PASSWORD: ${{ secrets.SMTP_PASSWORD }}
          RECIPIENT_EMAILS: ${{ secrets.RECIPIENT_EMAILS }}
          VAULT_PACKAGE_LOCATION: ${{ secrets.VAULT_PACKAGE_LOCATION }}
          DMS_KEY: ${{ secrets.DMS_KEY }}
          DAYS: ${{ steps.checkin.outputs.days_since }}
        run: |
          set -euo pipefail
          cat > message.txt <<EOF
          From: $SMTP_USERNAME
          To: $RECIPIENT_EMAILS
          Subject: Important: Access Information Vault
          MIME-Version: 1.0
          Content-Type: text/plain; charset=UTF-8

[[.ReleaseBody]]
          EOF
          sed 's/$/\r/' message.txt > message.eml
          rcpts=()
          IFS=',' read -ra addrs <<< "$RECIPIENT_EMAILS"
          for a in "${addrs[@]}"; do
            a=$(echo "$a" | xargs)
            [ -n "$a" ] && rcpts+=(--mail-rcpt "$a")
          done
          curl --silent --show-error --ssl-reqd \
            --url "[[.Scheme]]://$SMTP_SERVER:[[.SMTPPort]]" \
            --user "$SMTP_USERNAME:$SMTP_PASSWORD" \
            --mail-from "$SMTP_USERNAME" \
            "${rcpts[@]}" \
            --upload-file message.eml
`

// indentForRunBlock indents every non-empty line to the workflow's `run: |` block
// column (10 spaces), so the YAML strip leaves the heredoc body at column 0.
func indentForRunBlock(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = "          " + l
		}
	}
	return strings.Join(lines, "\n")
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

// GenerateGitHubDMSWorkflowFile writes the DMS workflow to an output directory.
// This is for a standalone DMS repo (separate from vault storage).
func GenerateGitHubDMSWorkflowFile(outputDir string, cfg *SwitchConfig) error {
	workflowDir := filepath.Join(outputDir, ".github", "workflows")
	if err := os.MkdirAll(workflowDir, 0755); err != nil {
		return fmt.Errorf("creating workflow dir: %w", err)
	}

	content := GenerateGitHubDMSWorkflow(cfg)
	path := filepath.Join(workflowDir, "deadman.yml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("writing workflow: %w", err)
	}

	return nil
}
