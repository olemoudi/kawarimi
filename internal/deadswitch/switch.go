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

// Evaluate runs the full switch evaluation: read check-in, determine stage, act.
func Evaluate(vaultDir string, switchCfg *SwitchConfig, appDir string) error {
	daysSince, err := DaysSinceCheckin(vaultDir)
	if err != nil {
		return fmt.Errorf("evaluating switch: %w", err)
	}

	// Check if already triggered
	triggeredPath := filepath.Join(appDir, "switch-triggered")
	if _, err := os.Stat(triggeredPath); err == nil {
		return nil // Already triggered, don't send again
	}

	stage := EvaluateStage(daysSince, switchCfg)

	switch stage {
	case StageNormal:
		return nil

	case StageWarning1:
		return SendEmail(switchCfg, []string{switchCfg.UserEmail},
			"Kawarimi: Missed check-in",
			fmt.Sprintf("You haven't checked in for %d days.\n\nRun 'kawarimi checkin' to reset the timer.\n\nIf you don't check in by day %d, your family will be notified.",
				daysSince, switchCfg.FinalDays))

	case StageWarning2:
		return SendEmail(switchCfg, []string{switchCfg.UserEmail},
			"URGENT: Kawarimi check-in overdue",
			fmt.Sprintf("You haven't checked in for %d days.\n\nRun 'kawarimi checkin' IMMEDIATELY to prevent passphrase release.\n\nThe passphrase will be sent to your designated recipients on day %d.",
				daysSince, switchCfg.FinalDays))

	case StageFinal:
		if err := triggerFinalRelease(switchCfg, appDir); err != nil {
			return err
		}
		// Mark as triggered
		return os.WriteFile(triggeredPath, []byte(time.Now().UTC().Format(time.RFC3339)), 0600)
	}

	return nil
}

func triggerFinalRelease(switchCfg *SwitchConfig, appDir string) error {
	// Read the stored passphrase from switch-payload.age
	passphrase, err := DecryptSwitchPayload(appDir)
	if err != nil {
		return fmt.Errorf("decrypting switch payload: %w", err)
	}

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

