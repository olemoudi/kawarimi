package gui

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/olemoudi/kawarimi/internal/config"
	"github.com/olemoudi/kawarimi/internal/crypto"
	"github.com/olemoudi/kawarimi/internal/deadswitch"
	"github.com/olemoudi/kawarimi/internal/github"
	"github.com/olemoudi/kawarimi/internal/setup"
	"github.com/olemoudi/kawarimi/internal/vault"
)

// handlePasswordStrength scores a candidate vault password for the live meter.
// The estimate never leaves this machine — the GUI server is loopback-only.
func (s *server) handlePasswordStrength(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	writeJSON(w, http.StatusOK, crypto.EstimatePasswordStrength(body.Password))
}

// handleInit creates a brand-new vault and returns the one-time secrets to display.
// It then unlocks the session with the same password so the wizard can add entries.
func (s *server) handleInit(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var body struct {
		Password   string `json:"password"`
		VaultDir   string `json:"vaultDir"`
		AcceptWeak bool   `json:"acceptWeak"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if strings.TrimSpace(body.Password) == "" {
		writeError(w, http.StatusBadRequest, "a password is required")
		return
	}
	// The client-side meter gates weak passwords behind an explicit override;
	// enforce the same here so the meter cannot be bypassed.
	if crypto.EstimatePasswordStrength(body.Password).Level < crypto.AcceptableStrengthLevel && !body.AcceptWeak {
		writeError(w, http.StatusBadRequest, "weak_password")
		return
	}

	vaultDir := strings.TrimSpace(body.VaultDir)
	if vaultDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		vaultDir = filepath.Join(home, "kawarimi-vault")
	}

	secrets, err := setup.InitVault(setup.InitOptions{VaultDir: vaultDir, Password: body.Password})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Unlock the freshly created vault so the wizard can add entries. Best-effort:
	// a failure here does not undo a successful init.
	_ = s.sess.unlock(body.Password)

	writeJSON(w, http.StatusOK, map[string]any{
		"vaultDir":            secrets.VaultDir,
		"mnemonic":            secrets.MnemonicWords,
		"recoveryCode":        secrets.RecoveryCode,
		"recipientPassphrase": secrets.RecipientPassphrase,
	})
}

type switchSetupRequest struct {
	SMTPServer           string   `json:"smtpServer"`
	SMTPPort             int      `json:"smtpPort"`
	SMTPUsername         string   `json:"smtpUsername"`
	SMTPPassword         string   `json:"smtpPassword"`
	SenderEmail          string   `json:"senderEmail"`
	UserEmail            string   `json:"userEmail"`
	Recipients           []string `json:"recipients"`
	Warning1Days         int      `json:"warning1Days"`
	Warning2Days         int      `json:"warning2Days"`
	FinalDays            int      `json:"finalDays"`
	VaultPackageLocation string   `json:"vaultPackageLocation"`
	TelegramBotToken     string   `json:"telegramBotToken"`
	TelegramChatID       string   `json:"telegramChatId"`
	IMAPServer           string   `json:"imapServer"`
	IMAPPort             int      `json:"imapPort"`
	LocalRelease         bool     `json:"localRelease"`
}

// handleSwitchSetup stores the switch config + payload for a V4 vault. It does not
// touch GitHub — that is the separate cloud step.
func (s *server) handleSwitchSetup(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req switchSetupRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	cfg, err := config.Load()
	if err != nil {
		writeError(w, http.StatusBadRequest, "no vault configured — create a vault first")
		return
	}
	appDir, err := config.AppDirPath()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !vault.IsV2Vault(cfg.VaultDir) {
		writeError(w, http.StatusBadRequest, "the browser wizard supports V4 vaults only")
		return
	}
	if _, err := os.Stat(filepath.Join(appDir, "dms-key")); err != nil {
		writeError(w, http.StatusBadRequest, "no DMS key found — create the vault with this wizard first")
		return
	}

	sc, msg := buildSwitchConfig(req)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	// Order matters: the payload store generates the switch identity that
	// SaveSwitchConfig then uses to encrypt the config.
	if err := setup.StoreSwitchPayloadForMode(appDir, req.LocalRelease); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := deadswitch.SaveSwitchConfig(appDir, sc); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "cloudOnly": !req.LocalRelease})
}

// buildSwitchConfig validates a request and maps it to a SwitchConfig, or returns a
// non-empty error message.
func buildSwitchConfig(req switchSetupRequest) (*deadswitch.SwitchConfig, string) {
	sc := deadswitch.DefaultSwitchConfig()

	sc.SMTPServer = strings.TrimSpace(req.SMTPServer)
	sc.SMTPUsername = strings.TrimSpace(req.SMTPUsername)
	sc.SMTPPassword = req.SMTPPassword
	sc.SenderEmail = strings.TrimSpace(req.SenderEmail)
	if sc.SenderEmail == "" {
		sc.SenderEmail = sc.SMTPUsername
	}
	if req.SMTPPort > 0 {
		sc.SMTPPort = req.SMTPPort
	}
	sc.UserEmail = strings.TrimSpace(req.UserEmail)

	for _, rcpt := range req.Recipients {
		if r := strings.TrimSpace(rcpt); r != "" {
			sc.Recipients = append(sc.Recipients, r)
		}
	}
	if req.Warning1Days > 0 {
		sc.Warning1Days = req.Warning1Days
	}
	if req.Warning2Days > 0 {
		sc.Warning2Days = req.Warning2Days
	}
	if req.FinalDays > 0 {
		sc.FinalDays = req.FinalDays
	}
	sc.VaultPackageLocation = strings.TrimSpace(req.VaultPackageLocation)
	sc.TelegramBotToken = strings.TrimSpace(req.TelegramBotToken)
	sc.TelegramChatID = strings.TrimSpace(req.TelegramChatID)
	sc.IMAPServer = strings.TrimSpace(req.IMAPServer)
	if req.IMAPPort > 0 {
		sc.IMAPPort = req.IMAPPort
	}

	sc.PingChannels = []string{"email"}
	if sc.TelegramBotToken != "" {
		sc.PingChannels = append(sc.PingChannels, "telegram")
	}

	// Validation.
	if sc.SMTPServer == "" || sc.SMTPUsername == "" {
		return nil, "SMTP server and username are required"
	}
	if sc.UserEmail == "" {
		return nil, "your own email (for warnings) is required"
	}
	if len(sc.Recipients) == 0 {
		return nil, "at least one recipient email is required"
	}
	if sc.VaultPackageLocation == "" {
		return nil, "a vault package location (where recipients download the package) is required"
	}
	if !(sc.Warning1Days < sc.Warning2Days && sc.Warning2Days < sc.FinalDays) {
		return nil, "thresholds must increase: warning 1 < warning 2 < final release"
	}
	return sc, ""
}

// handleSwitchCloud creates the private DMS repo, sets its Actions secrets, and
// arms the switch (pushes the workflow + heartbeat over SSH). The GitHub token is
// held only for the duration of this request.
func (s *server) handleSwitchCloud(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var body struct {
		GitHubToken string `json:"githubToken"`
		RepoName    string `json:"repoName"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	token := strings.TrimSpace(body.GitHubToken)
	repoName := strings.TrimSpace(body.RepoName)
	if token == "" || repoName == "" {
		writeError(w, http.StatusBadRequest, "a GitHub token and repository name are required")
		return
	}

	cfg, err := config.Load()
	if err != nil {
		writeError(w, http.StatusBadRequest, "no vault configured")
		return
	}
	appDir, err := config.AppDirPath()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !deadswitch.IsSwitchConfigured(appDir) {
		writeError(w, http.StatusBadRequest, "configure the dead man's switch before the cloud step")
		return
	}
	sc, err := deadswitch.LoadSwitchConfig(appDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	dmsKeyData, err := os.ReadFile(filepath.Join(appDir, "dms-key"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "no DMS key on this machine — re-run the vault step or 'kawarimi switch rekey'")
		return
	}

	s.sess.setGitHubToken(token)
	defer s.sess.clearGitHubToken()

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	client := github.NewClient(token)
	repo, err := client.CreatePrivateRepo(ctx, repoName)
	if err != nil {
		writeError(w, http.StatusBadGateway, "creating GitHub repo: "+err.Error())
		return
	}

	secrets := map[string]string{
		"SMTP_SERVER":            sc.SMTPServer,
		"SMTP_USERNAME":          sc.SMTPUsername,
		"SMTP_PASSWORD":          sc.SMTPPassword,
		"USER_EMAIL":             sc.UserEmail,
		"RECIPIENT_EMAILS":       strings.Join(sc.Recipients, ","),
		"VAULT_PACKAGE_LOCATION": sc.VaultPackageLocation,
		"DMS_KEY":                strings.TrimSpace(string(dmsKeyData)),
	}
	if sc.TelegramBotToken != "" {
		secrets["TELEGRAM_BOT_TOKEN"] = sc.TelegramBotToken
		secrets["TELEGRAM_CHAT_ID"] = sc.TelegramChatID
	}
	if err := client.SetActionsSecrets(ctx, repo.Owner, repo.Name, secrets); err != nil {
		writeError(w, http.StatusBadGateway, "setting Actions secrets: "+err.Error())
		return
	}

	// Arm the switch: push the workflow + heartbeat over SSH (uses the owner's SSH key).
	if _, err := setup.SeedSwitch(cfg, sc, repo.SSHURL, false); err != nil {
		writeError(w, http.StatusBadGateway,
			"repo and secrets were configured, but arming the switch failed (check that your SSH key is registered with GitHub, then retry): "+err.Error())
		return
	}

	// Cloud-only default: now that the secret is set in GitHub, drop the local copy.
	if deadswitch.SwitchIsCloudOnly(appDir) {
		_ = os.Remove(filepath.Join(appDir, "dms-key"))
	}

	names := make([]string, 0, len(secrets))
	for n := range secrets {
		names = append(names, n)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"repo":       repo.Owner + "/" + repo.Name,
		"sshUrl":     repo.SSHURL,
		"secretsSet": names,
	})
}
