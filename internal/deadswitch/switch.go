package deadswitch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/olemoudi/kawarimi/internal/atomicfile"
	"github.com/olemoudi/kawarimi/internal/copytext"
	"github.com/olemoudi/kawarimi/internal/vault"
)

// Stage represents the escalation stage of the dead man's switch.
type Stage int

const (
	StageNormal   Stage = iota // Within check-in interval
	StageWarning1              // First warning (user only)
	StageWarning2              // Urgent warning (user only)
	StageFinal                 // Release to recipients
)

// SwitchConfig holds the dead man's switch configuration.
type SwitchConfig struct {
	SMTPServer   string   `json:"smtp_server"`
	SMTPPort     int      `json:"smtp_port"`
	SMTPUsername string   `json:"smtp_username"`
	SMTPPassword string   `json:"smtp_password"`
	SenderEmail  string   `json:"sender_email"`
	UserEmail    string   `json:"user_email"`
	Recipients   []string `json:"recipients"`
	// Escalation thresholds in days
	Warning1Days int `json:"warning1_days"`
	Warning2Days int `json:"warning2_days"`
	FinalDays    int `json:"final_days"`
	// Physical passphrase location (used in GitHub Actions notification)
	PassphraseLocation string `json:"passphrase_location"`
	// Vault repo URL (for notification emails)
	VaultRepoURL string `json:"vault_repo_url"`
	// Telegram bot configuration
	TelegramBotToken string `json:"telegram_bot_token,omitempty"`
	TelegramChatID   string `json:"telegram_chat_id,omitempty"`
	// Ping channels: "email", "telegram"
	PingChannels []string `json:"ping_channels,omitempty"`
	// Mnemonic delivery mode: "email" (include words in email) or "physical" (reference location only)
	MnemonicDelivery string `json:"mnemonic_delivery,omitempty"`
	// Custom delivery instructions for how receiver gets the vault files
	DeliveryInstructions string `json:"delivery_instructions,omitempty"`
	// IMAP configuration for email reply check-in
	IMAPServer string `json:"imap_server,omitempty"`
	IMAPPort   int    `json:"imap_port,omitempty"`
	// Vault package location(s) — where recipients can download the vault package
	VaultPackageLocation string `json:"vault_package_location,omitempty"`
}

// DefaultSwitchConfig returns a config with default escalation thresholds.
func DefaultSwitchConfig() *SwitchConfig {
	return &SwitchConfig{
		SMTPPort:     587,
		Warning1Days: 14,
		Warning2Days: 21,
		FinalDays:    30,
	}
}

// ReadLastCheckin reads the last check-in timestamp from the vault.
func ReadLastCheckin(vaultDir string) (time.Time, error) {
	path := filepath.Join(vaultDir, vault.LastCheckinFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, fmt.Errorf("reading last check-in: %w", err)
	}
	ts := strings.TrimSpace(string(data))
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return time.Time{}, fmt.Errorf("parsing check-in timestamp: %w", err)
	}
	return t, nil
}

// DaysSinceCheckin returns the number of days since the last check-in.
func DaysSinceCheckin(vaultDir string) (int, error) {
	lastCheckin, err := ReadLastCheckin(vaultDir)
	if err != nil {
		return -1, err
	}
	return int(time.Since(lastCheckin).Hours() / 24), nil
}

// EvaluateStage determines the current escalation stage based on days since check-in.
func EvaluateStage(daysSince int, cfg *SwitchConfig) Stage {
	switch {
	case daysSince >= cfg.FinalDays:
		return StageFinal
	case daysSince >= cfg.Warning2Days:
		return StageWarning2
	case daysSince >= cfg.Warning1Days:
		return StageWarning1
	default:
		return StageNormal
	}
}

// hasPingChannel checks if a channel is configured for pinging.
func hasPingChannel(cfg *SwitchConfig, channel string) bool {
	// If no channels configured, default to email
	if len(cfg.PingChannels) == 0 {
		return channel == "email"
	}
	for _, c := range cfg.PingChannels {
		if c == channel {
			return true
		}
	}
	return false
}

// Evaluate runs the full switch evaluation: read check-in, determine stage, act.
// targets carries the vault dir plus the optional DMS repo so that auto check-ins
// (Telegram /alive, IMAP ALIVE) propagate to the cloud heartbeat, not just the
// local file.
func Evaluate(targets CheckinTargets, switchCfg *SwitchConfig, appDir string) error {
	vaultDir := targets.VaultDir

	// Check if already triggered
	triggeredPath := filepath.Join(appDir, "switch-triggered")
	if _, err := os.Stat(triggeredPath); err == nil {
		return nil // Already triggered, don't send again
	}

	// Check Telegram for /alive replies before evaluating
	if switchCfg.TelegramBotToken != "" && switchCfg.TelegramChatID != "" {
		lastCheckin, err := ReadLastCheckin(vaultDir)
		if err == nil {
			alive, err := CheckForAlive(switchCfg.TelegramBotToken, switchCfg.TelegramChatID, lastCheckin, appDir)
			if err == nil && alive {
				autoCheckin(targets, switchCfg, "Telegram")
			}
		}
	}

	// Check IMAP for email replies
	if switchCfg.IMAPServer != "" {
		lastCheckin, err := ReadLastCheckin(vaultDir)
		if err == nil {
			alive, err := CheckIMAPForAlive(switchCfg, lastCheckin)
			if err == nil && alive {
				autoCheckin(targets, switchCfg, "IMAP")
			}
		}
	}

	daysSince, err := DaysSinceCheckin(vaultDir)
	if err != nil {
		return fmt.Errorf("evaluating switch: %w", err)
	}

	stage := EvaluateStage(daysSince, switchCfg)
	overdueAnchor := filepath.Join(appDir, "first-overdue-at")

	switch stage {
	case StageNormal:
		// Healthy again (checked in, or a transient clock skew corrected itself):
		// reset the overdue ratchet.
		os.Remove(overdueAnchor)
		return nil

	case StageWarning1:
		recordFirstOverdue(overdueAnchor)
		return sendPing(switchCfg, daysSince, false)

	case StageWarning2:
		recordFirstOverdue(overdueAnchor)
		return sendPing(switchCfg, daysSince, true)

	case StageFinal:
		recordFirstOverdue(overdueAnchor)
		// Clock-jump guard: only release locally once the switch has been overdue for
		// enough REAL elapsed time (measured from the anchor set when it first went
		// overdue), so a forward clock jump cannot skip the warning ladder and
		// disclose the key on a single run. The cloud DMS (correct NTP time on the
		// runner) remains the authoritative post-mortem trigger.
		if !overdueLongEnough(overdueAnchor, switchCfg) {
			fmt.Fprintln(os.Stderr, "dead man's switch reached the final stage, but the overdue period is not yet confirmed by real elapsed time (possible clock jump) — alerting the owner instead of releasing")
			return sendPing(switchCfg, daysSince, true)
		}
		if err := triggerFinalRelease(switchCfg, appDir); err != nil {
			return err
		}
		return atomicfile.WriteFile(triggeredPath, []byte(time.Now().UTC().Format(time.RFC3339)), 0600)
	}

	return nil
}

// recordFirstOverdue stamps the first wall-clock time the switch was observed overdue
// (if not already stamped), as the anchor for the clock-jump ratchet.
func recordFirstOverdue(path string) {
	if _, err := os.Stat(path); err == nil {
		return
	}
	_ = atomicfile.WriteFile(path, []byte(time.Now().UTC().Format(time.RFC3339)), 0600)
}

// overdueLongEnough reports whether enough real time has elapsed since the switch
// first went overdue to justify a local final release — the (FinalDays-Warning1Days)
// span the warning ladder normally occupies, and at least one full day.
func overdueLongEnough(path string, cfg *SwitchConfig) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	first, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
	if err != nil {
		return false
	}
	minReal := time.Duration(cfg.FinalDays-cfg.Warning1Days) * 24 * time.Hour
	if minReal < 24*time.Hour {
		minReal = 24 * time.Hour
	}
	return time.Since(first) >= minReal
}

// autoCheckin records an auto check-in triggered by an ALIVE reply. If the local
// heartbeat refreshed but the cloud push failed, it alerts the owner: otherwise the
// local switch goes quiet (owner looks fine here) while the cloud heartbeat stays
// stale and could fire while the owner is alive — a dangerous split brain.
func autoCheckin(targets CheckinTargets, cfg *SwitchConfig, source string) {
	pushed, err := RecordCheckin(targets, time.Now())
	if err != nil {
		fmt.Fprintf(os.Stderr, "auto check-in from %s: %v\n", source, err)
	}
	if !pushed && targets.DMSRemote != "" {
		_ = SendEmail(cfg, []string{cfg.UserEmail},
			"Kawarimi: check-in did not reach the cloud",
			fmt.Sprintf("An automatic check-in (via %s) updated this machine but could NOT reach the cloud dead man's switch.\n\nIf the cloud heartbeat stays stale the switch may fire while you are alive. Run 'kawarimi checkin' and 'kawarimi switch verify' from a connected machine.", source))
		if cfg.TelegramBotToken != "" && cfg.TelegramChatID != "" {
			_ = SendTelegramMessage(cfg.TelegramBotToken, cfg.TelegramChatID,
				"Kawarimi: an /alive check-in did not reach the cloud switch. Please run 'kawarimi checkin' from a connected machine.")
		}
	}
}

// AlertIfRemoteStale runs a health check and, if the cloud switch looks unhealthy
// (stale remote heartbeat, or a missing/outdated workflow), emails the owner at most
// once a day. Running it from the systemd `evaluate` timer turns the otherwise
// manual `switch verify` into an automatic tripwire, so a silently-broken cloud
// switch is caught. Best-effort: it never alerts when the remote simply can't be
// reached (this machine may be offline), to avoid false alarms.
func AlertIfRemoteStale(targets CheckinTargets, cfg *SwitchConfig, appDir string) {
	if targets.DMSRemote == "" {
		return
	}
	report, err := Verify(targets, cfg, appDir)
	if err != nil || report.RemoteCheckinErr != nil {
		return // couldn't complete the check / reach the remote — don't false-alarm
	}
	markerPath := filepath.Join(appDir, "remote-alert-at")
	if report.WorkflowPresent && report.WorkflowUpToDate && !report.RemoteStale {
		os.Remove(markerPath) // healthy again
		return
	}
	// Dedup: at most one alert per ~day.
	if data, rerr := os.ReadFile(markerPath); rerr == nil {
		if last, perr := time.Parse(time.RFC3339, strings.TrimSpace(string(data))); perr == nil && time.Since(last) < 20*time.Hour {
			return
		}
	}
	body := "Your Kawarimi cloud dead man's switch looks unhealthy:\n"
	if !report.WorkflowPresent {
		body += "  - the release workflow is missing from the DMS repo\n"
	} else if !report.WorkflowUpToDate {
		body += "  - the release workflow is out of date\n"
	}
	if report.RemoteStale {
		body += "  - the cloud heartbeat is stale (your check-ins are not reaching it)\n"
	}
	body += "\nRun 'kawarimi switch verify' and 'kawarimi switch seed' from a connected machine."
	if err := SendEmail(cfg, []string{cfg.UserEmail}, "Kawarimi: cloud dead man's switch needs attention", body); err == nil {
		_ = atomicfile.WriteFile(markerPath, []byte(time.Now().UTC().Format(time.RFC3339)), 0600)
	}
}

// sendPing sends check-in reminders on all configured channels.
func sendPing(cfg *SwitchConfig, daysSince int, urgent bool) error {
	var firstErr error

	if hasPingChannel(cfg, "email") {
		subject := "Kawarimi: Missed check-in"
		body := fmt.Sprintf("You haven't checked in for %d days.\n\nRun 'kawarimi checkin' to reset the timer.\n\nIf you don't check in by day %d, your family will be notified.",
			daysSince, cfg.FinalDays)
		if urgent {
			subject = "URGENT: Kawarimi check-in overdue"
			body = fmt.Sprintf("You haven't checked in for %d days.\n\nRun 'kawarimi checkin' IMMEDIATELY to prevent vault release.\n\nThe vault will be released to your designated recipients on day %d.\n\nYou can also reply to this email with subject 'ALIVE' to check in.",
				daysSince, cfg.FinalDays)
		}
		if err := SendEmail(cfg, []string{cfg.UserEmail}, subject, body); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("email ping: %w", err)
		}
	}

	if hasPingChannel(cfg, "telegram") && cfg.TelegramBotToken != "" {
		var err error
		if urgent {
			err = SendTelegramWarning(cfg, daysSince)
		} else {
			err = SendTelegramPing(cfg, daysSince)
		}
		if err != nil && firstErr == nil {
			firstErr = fmt.Errorf("telegram ping: %w", err)
		}
	}

	return firstErr
}

func triggerFinalRelease(switchCfg *SwitchConfig, appDir string) error {
	// Read the stored payload from switch-payload.age
	payload, err := DecryptSwitchPayload(appDir)
	if err != nil {
		return fmt.Errorf("decrypting switch payload: %w", err)
	}

	// Detect payload type by prefix
	if strings.HasPrefix(payload, "CLOUDONLY:") {
		return triggerFinalReleaseCloudOnly(switchCfg)
	}
	if strings.HasPrefix(payload, "DMSKEY:") {
		return triggerFinalReleaseV4(switchCfg, payload)
	}

	// Pre-V4 payloads (SEALED:/MNEMONIC:/bare passphrase) emailed secrets that the
	// V4 key-split deliberately never sends. No public release ever shipped them,
	// so rather than keep untested release paths alive, fail loudly toward the
	// OWNER: nothing is emailed to recipients, and the owner is told how to fix it.
	err = fmt.Errorf("switch payload predates the V4 key-split — run 'kawarimi switch rekey' to re-arm; nothing was sent to recipients")
	_ = SendEmail(switchCfg, []string{switchCfg.UserEmail},
		"Kawarimi: dead man's switch could NOT release (outdated payload)",
		"The switch reached its final stage but the stored payload predates the V4\n"+
			"key-split, which this version no longer delivers.\n\n"+
			"Nothing was sent to your recipients.\n\n"+
			"If you are seeing this, run 'kawarimi checkin', then 'kawarimi switch rekey'\n"+
			"to upgrade the switch payload.")
	return err
}

// triggerFinalReleaseCloudOnly runs when this machine reaches the final stage but
// holds no DMS key. It alerts the owner rather than releasing (the cloud does that).
func triggerFinalReleaseCloudOnly(switchCfg *SwitchConfig) error {
	subject := "Kawarimi: final stage reached (cloud DMS will release)"
	body := "This machine reached the final dead man's switch stage, but it is configured\n" +
		"cloud-only and holds no DMS key, so it will not deliver anything itself. The\n" +
		"GitHub Actions dead man's switch is responsible for delivering the key to your\n" +
		"recipients.\n\n" +
		"If you are alive and seeing this, run 'kawarimi checkin'.\n" +
		"If the cloud switch might be misconfigured, run 'kawarimi switch verify'."
	return SendEmail(switchCfg, []string{switchCfg.UserEmail}, subject, body)
}

func triggerFinalReleaseV4(switchCfg *SwitchConfig, payload string) error {
	dmsKeyBase64 := strings.TrimPrefix(payload, "DMSKEY:")

	location := switchCfg.VaultPackageLocation
	if location == "" {
		location = switchCfg.DeliveryInstructions
	}
	if location == "" && switchCfg.VaultRepoURL != "" {
		location = switchCfg.VaultRepoURL
	}
	if location == "" {
		location = "(ask the family where the vault package is stored)"
	}

	subject := "Importante: acceso a la caja fuerte / Important: vault access"
	body := copytext.ReleaseEmailBody(location, dmsKeyBase64)

	return SendEmail(switchCfg, switchCfg.Recipients, subject, body)
}

// DecryptSwitchPayload reads the vault passphrase from the switch payload.
func DecryptSwitchPayload(appDir string) (string, error) {
	identityPath := filepath.Join(appDir, "switch-identity.key")
	payloadPath := filepath.Join(appDir, "switch-payload.age")

	identity, err := os.ReadFile(identityPath)
	if err != nil {
		return "", fmt.Errorf("reading switch identity: %w", err)
	}

	payload, err := os.ReadFile(payloadPath)
	if err != nil {
		return "", fmt.Errorf("reading switch payload: %w", err)
	}

	return decryptWithX25519(payload, strings.TrimSpace(string(identity)))
}

// StoreSwitchMnemonic encrypts and stores mnemonic words for the switch (v2).
func StoreSwitchMnemonic(appDir string, words []string) error {
	payload := "MNEMONIC:" + strings.Join(words, " ")
	return StoreSwitchPayload(appDir, payload)
}

// StoreSwitchSealedPayload encrypts and stores a sealed payload for the switch (v3).
// The sealed payload is an age ciphertext that can only be decrypted with the
// recipient passphrase (which the DMS does not have).
func StoreSwitchSealedPayload(appDir string, sealedPayloadBase64 string) error {
	payload := "SEALED:" + sealedPayloadBase64
	return StoreSwitchPayload(appDir, payload)
}

// StoreSwitchDMSKey encrypts and stores the DMS key for the switch (v4).
// The DMS only stores this key, not the sealed payload. When triggered, the key
// is sent to recipients who combine it with their passphrase to unseal the vault.
func StoreSwitchDMSKey(appDir string, dmsKeyBase64 string) error {
	payload := "DMSKEY:" + dmsKeyBase64
	return StoreSwitchPayload(appDir, payload)
}

// StoreSwitchCloudOnly stores a marker instead of the DMS key. In this mode the
// local machine holds no key and never performs the final release — that is left to
// the cloud (GitHub Actions) — so a compromise of this machine cannot yield the key.
func StoreSwitchCloudOnly(appDir string) error {
	return StoreSwitchPayload(appDir, "CLOUDONLY:")
}

// SwitchIsCloudOnly reports whether the switch is configured for cloud-only release.
func SwitchIsCloudOnly(appDir string) bool {
	payload, err := DecryptSwitchPayload(appDir)
	if err != nil {
		return false
	}
	return strings.HasPrefix(payload, "CLOUDONLY:")
}

// StoreSwitchPayload encrypts and stores the vault passphrase for the switch.
func StoreSwitchPayload(appDir string, passphrase string) error {
	pubKey, privKey, err := generateX25519KeyPair()
	if err != nil {
		return fmt.Errorf("generating key pair: %w", err)
	}

	// Encrypt the payload first, so we never replace the identity with a new one
	// unless we also have its matching payload ready to write.
	encrypted, err := encryptWithX25519([]byte(passphrase), pubKey)
	if err != nil {
		return fmt.Errorf("encrypting switch payload: %w", err)
	}

	identityPath := filepath.Join(appDir, "switch-identity.key")
	if err := atomicfile.WriteFile(identityPath, []byte(privKey+"\n"), 0600); err != nil {
		return fmt.Errorf("writing switch identity: %w", err)
	}

	payloadPath := filepath.Join(appDir, "switch-payload.age")
	if err := atomicfile.WriteFile(payloadPath, encrypted, 0600); err != nil {
		return fmt.Errorf("writing switch payload: %w", err)
	}

	return nil
}

// SwitchConfigPath returns the path for the encrypted switch config.
func SwitchConfigPath(appDir string) string {
	return filepath.Join(appDir, "switch-config.age")
}

// IsSwitchConfigured returns true if the switch config files exist.
func IsSwitchConfigured(appDir string) bool {
	if _, err := os.Stat(filepath.Join(appDir, "switch-identity.key")); err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(appDir, "switch-payload.age")); err != nil {
		return false
	}
	return true
}

// SaveSwitchConfig encrypts and saves the switch configuration.
func SaveSwitchConfig(appDir string, cfg *SwitchConfig) error {
	identityPath := filepath.Join(appDir, "switch-identity.key")
	identity, err := os.ReadFile(identityPath)
	if err != nil {
		return fmt.Errorf("reading switch identity for config encryption: %w", err)
	}

	// Extract public key from identity
	pubKey, err := pubKeyFromIdentity(strings.TrimSpace(string(identity)))
	if err != nil {
		return fmt.Errorf("extracting public key: %w", err)
	}

	data, err := marshalJSON(cfg)
	if err != nil {
		return fmt.Errorf("marshaling switch config: %w", err)
	}

	encrypted, err := encryptWithX25519(data, pubKey)
	if err != nil {
		return fmt.Errorf("encrypting switch config: %w", err)
	}

	return atomicfile.WriteFile(SwitchConfigPath(appDir), encrypted, 0600)
}

// LoadSwitchConfig decrypts and loads the switch configuration.
func LoadSwitchConfig(appDir string) (*SwitchConfig, error) {
	identityPath := filepath.Join(appDir, "switch-identity.key")
	identity, err := os.ReadFile(identityPath)
	if err != nil {
		return nil, fmt.Errorf("reading switch identity: %w", err)
	}

	configPath := SwitchConfigPath(appDir)
	ciphertext, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("reading switch config: %w", err)
	}

	plaintext, err := decryptWithX25519(ciphertext, strings.TrimSpace(string(identity)))
	if err != nil {
		return nil, fmt.Errorf("decrypting switch config: %w", err)
	}

	var cfg SwitchConfig
	if err := unmarshalJSON([]byte(plaintext), &cfg); err != nil {
		return nil, fmt.Errorf("parsing switch config: %w", err)
	}

	return &cfg, nil
}
