package deadswitch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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
			alive, err := CheckForAlive(switchCfg.TelegramBotToken, switchCfg.TelegramChatID, lastCheckin)
			if err == nil && alive {
				autoCheckin(targets, "Telegram")
			}
		}
	}

	// Check IMAP for email replies
	if switchCfg.IMAPServer != "" {
		lastCheckin, err := ReadLastCheckin(vaultDir)
		if err == nil {
			alive, err := CheckIMAPForAlive(switchCfg, lastCheckin)
			if err == nil && alive {
				autoCheckin(targets, "IMAP")
			}
		}
	}

	daysSince, err := DaysSinceCheckin(vaultDir)
	if err != nil {
		return fmt.Errorf("evaluating switch: %w", err)
	}

	stage := EvaluateStage(daysSince, switchCfg)

	switch stage {
	case StageNormal:
		return nil

	case StageWarning1:
		return sendPing(switchCfg, daysSince, false)

	case StageWarning2:
		return sendPing(switchCfg, daysSince, true)

	case StageFinal:
		if err := triggerFinalRelease(switchCfg, appDir); err != nil {
			return err
		}
		return os.WriteFile(triggeredPath, []byte(time.Now().UTC().Format(time.RFC3339)), 0600)
	}

	return nil
}

// autoCheckin records an auto check-in triggered by an ALIVE reply. It is
// best-effort: a cloud push failure is logged to stderr (the systemd journal)
// but does not abort evaluation.
func autoCheckin(targets CheckinTargets, source string) {
	if _, err := RecordCheckin(targets, time.Now()); err != nil {
		fmt.Fprintf(os.Stderr, "auto check-in from %s: cloud DMS push failed: %v\n", source, err)
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
	if strings.HasPrefix(payload, "DMSKEY:") {
		return triggerFinalReleaseV4(switchCfg, payload)
	}
	if strings.HasPrefix(payload, "SEALED:") {
		return triggerFinalReleaseV3(switchCfg, payload)
	}
	if strings.HasPrefix(payload, "MNEMONIC:") {
		return triggerFinalReleaseV2(switchCfg, payload)
	}

	// V1: payload is a passphrase
	return triggerFinalReleaseV1(switchCfg, payload)
}

func triggerFinalReleaseV1(switchCfg *SwitchConfig, passphrase string) error {
	subject := "Important: Access Information Vault"
	body := fmt.Sprintf(`This is an automated message from the Kawarimi information vault.

The vault owner has not checked in for an extended period.

To access the encrypted information:

1. Locate the vault in one of these places:
   - Git repository: %s
   - USB drive (check with family for location)

2. Install the 'age' decryption tool:
   - Download from: https://github.com/FiloSottile/age/releases
   - Or on Mac: brew install age

3. Your decryption passphrase is:

   %s

4. Open a terminal and navigate to the vault directory.
   Read DECRYPT_INSTRUCTIONS.md for detailed steps.

5. Quick start:
   age -d manifest.age > manifest.json
   Then open manifest.json to see what's available.

IMPORTANT: Store this passphrase securely. Do not share it
beyond the intended recipients.
`, switchCfg.VaultRepoURL, passphrase)

	return SendEmail(switchCfg, switchCfg.Recipients, subject, body)
}

func triggerFinalReleaseV2(switchCfg *SwitchConfig, payload string) error {
	mnemonicStr := strings.TrimPrefix(payload, "MNEMONIC:")
	words := strings.Fields(mnemonicStr)

	// Delivery instructions
	deliverySection := "1. Locate the vault:\n"
	if switchCfg.DeliveryInstructions != "" {
		deliverySection += "   " + strings.ReplaceAll(switchCfg.DeliveryInstructions, "\n", "\n   ")
	} else if switchCfg.VaultRepoURL != "" {
		deliverySection += fmt.Sprintf("   Git repository: %s\n   Run: git clone %s", switchCfg.VaultRepoURL, switchCfg.VaultRepoURL)
	} else {
		deliverySection += "   Check with family for the vault location (USB drive, shared folder, etc.)"
	}

	// Mnemonic section
	var mnemonicSection string
	if switchCfg.MnemonicDelivery == "physical" {
		location := switchCfg.PassphraseLocation
		if location == "" {
			location = "a sealed envelope/card provided by the vault owner"
		}
		mnemonicSection = fmt.Sprintf(`3. Find the 8 mnemonic words at:
   %s
   Enter the words when prompted.`, location)
	} else {
		var wordList string
		for i, w := range words {
			wordList += fmt.Sprintf("   %d. %s\n", i+1, w)
		}
		mnemonicSection = fmt.Sprintf(`3. When prompted, enter these 8 mnemonic words:

%s
   If you also have a physical card/envelope with mnemonic words,
   use those instead (more secure).`, wordList)
	}

	subject := "Important: Access Information Vault"
	body := fmt.Sprintf(`This is an automated message from the Kawarimi information vault.

The vault owner has not checked in for an extended period.

HOW TO ACCESS THE VAULT:

%s

2. In the vault directory, find the kawarimi program for your
   computer (kawarimi-linux, kawarimi-macos, or kawarimi-windows.exe).

   Open a terminal/command prompt and run:
   ./kawarimi export --mnemonic ./decrypted/

%s

4. Your decrypted files will be in the ./decrypted/ directory.

IMPORTANT: Store the mnemonic words securely. Do not share them
beyond the intended recipients.
`, deliverySection, mnemonicSection)

	return SendEmail(switchCfg, switchCfg.Recipients, subject, body)
}

func triggerFinalReleaseV3(switchCfg *SwitchConfig, payload string) error {
	sealedBase64 := strings.TrimPrefix(payload, "SEALED:")

	// Vault package location
	locationSection := "1. Download the vault package:\n"
	if switchCfg.VaultPackageLocation != "" {
		locationSection += "   " + strings.ReplaceAll(switchCfg.VaultPackageLocation, "\n", "\n   ")
	} else if switchCfg.DeliveryInstructions != "" {
		locationSection += "   " + strings.ReplaceAll(switchCfg.DeliveryInstructions, "\n", "\n   ")
	} else if switchCfg.VaultRepoURL != "" {
		locationSection += fmt.Sprintf("   Git repository: %s\n   Run: git clone %s", switchCfg.VaultRepoURL, switchCfg.VaultRepoURL)
	} else {
		locationSection += "   Check with family for the vault package location."
	}

	subject := "Important: Access Information Vault"
	body := fmt.Sprintf(`This is an automated message from the Kawarimi information vault.

The vault owner has not checked in for an extended period.

HOW TO ACCESS THE VAULT:

%s

2. Extract the vault package (zip file).

3. Find the kawarimi program for your computer:
   - kawarimi-linux-amd64     (Linux)
   - kawarimi-darwin-arm64    (Mac with Apple Silicon)
   - kawarimi-windows-amd64.exe (Windows)

4. Open a terminal/command prompt and run:
   ./kawarimi export --sealed ./decrypted/

5. When prompted, paste this sealed payload:

%s

6. When prompted, enter the RECIPIENT PASSPHRASE from the
   physical card given to you by the vault owner.

7. Your decrypted files will be in the ./decrypted/ directory.

IMPORTANT: Keep the recipient passphrase card secure.
Do not share it beyond the intended recipients.
`, locationSection, sealedBase64)

	return SendEmail(switchCfg, switchCfg.Recipients, subject, body)
}

func triggerFinalReleaseV4(switchCfg *SwitchConfig, payload string) error {
	dmsKeyBase64 := strings.TrimPrefix(payload, "DMSKEY:")

	// Vault package location
	locationSection := "1. Download the vault package:\n"
	if switchCfg.VaultPackageLocation != "" {
		locationSection += "   " + strings.ReplaceAll(switchCfg.VaultPackageLocation, "\n", "\n   ")
	} else if switchCfg.DeliveryInstructions != "" {
		locationSection += "   " + strings.ReplaceAll(switchCfg.DeliveryInstructions, "\n", "\n   ")
	} else if switchCfg.VaultRepoURL != "" {
		locationSection += fmt.Sprintf("   Git repository: %s\n   Run: git clone %s", switchCfg.VaultRepoURL, switchCfg.VaultRepoURL)
	} else {
		locationSection += "   Check with family for the vault package location."
	}

	subject := "Important: Access Information Vault"
	body := fmt.Sprintf(`This is an automated message from the Kawarimi information vault.

The vault owner has not checked in for an extended period.

HOW TO ACCESS THE VAULT:

%s

2. Extract the vault package (zip file).

3. Find the kawarimi program for your computer:
   - kawarimi-linux-amd64     (Linux)
   - kawarimi-darwin-arm64    (Mac with Apple Silicon)
   - kawarimi-windows-amd64.exe (Windows)

4. Open a terminal/command prompt and run:
   ./kawarimi export --sealed ./decrypted/

5. When prompted, paste this DMS KEY:

%s

6. When prompted, enter the RECIPIENT PASSPHRASE from the
   physical card given to you by the vault owner.

7. Your decrypted files will be in the ./decrypted/ directory.

IMPORTANT: Keep the recipient passphrase card secure.
Do not share it beyond the intended recipients.
`, locationSection, dmsKeyBase64)

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

// StoreSwitchPayload encrypts and stores the vault passphrase for the switch.
func StoreSwitchPayload(appDir string, passphrase string) error {
	pubKey, privKey, err := generateX25519KeyPair()
	if err != nil {
		return fmt.Errorf("generating key pair: %w", err)
	}

	// Store identity (private key)
	identityPath := filepath.Join(appDir, "switch-identity.key")
	if err := os.WriteFile(identityPath, []byte(privKey+"\n"), 0600); err != nil {
		return fmt.Errorf("writing switch identity: %w", err)
	}

	// Encrypt and store payload
	encrypted, err := encryptWithX25519([]byte(passphrase), pubKey)
	if err != nil {
		return fmt.Errorf("encrypting switch payload: %w", err)
	}

	payloadPath := filepath.Join(appDir, "switch-payload.age")
	if err := os.WriteFile(payloadPath, encrypted, 0600); err != nil {
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

	return os.WriteFile(SwitchConfigPath(appDir), encrypted, 0600)
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
