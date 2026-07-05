// Package demo builds and drives the sandboxed "lifecycle theater" behind
// `kawarimi demo`: a throwaway HOME containing a real vault, a real armed cloud
// switch pointed at in-process mocks (SMTP, Telegram, GitHub, a local bare git
// repo as the cloud), and time-travel controls that backdate the heartbeat and run
// the real release engines. Nothing leaves the machine and everything is deleted
// on exit. It is OWNER-side only; the recipient path gains no code and no network
// calls from it.
//
// Time model: there is deliberately no fast clock in the production dead man's
// switch. The demo advances time by backdating the heartbeat through the real
// check-in path, then running "one day" of the real automation: the generated
// deadman.yml under the mini Actions runner (Linux) and the local evaluator tick.
package demo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/olemoudi/kawarimi/internal/config"
	"github.com/olemoudi/kawarimi/internal/crypto"
	"github.com/olemoudi/kawarimi/internal/deadswitch"
	"github.com/olemoudi/kawarimi/internal/github"
	"github.com/olemoudi/kawarimi/internal/setup"
	"github.com/olemoudi/kawarimi/internal/simenv"
	"github.com/olemoudi/kawarimi/internal/vault"
)

const (
	ownerEmail     = "owner@demo.kawarimi"
	recipientEmail = "recipient@demo.kawarimi"
	senderEmail    = "switch-bot@demo.kawarimi"
	demoChatID     = "424242"
	demoPassword   = "demo-sandbox-password"
)

// Options configure a demo world.
type Options struct {
	Version string
	// ForceLocalEngine skips the real-workflow cloud engine even where available,
	// so tests can exercise the local-evaluator path on any OS.
	ForceLocalEngine bool
}

// World is the demo sandbox plus everything needed to drive and observe it.
// All methods are safe for concurrent use.
type World struct {
	mu   sync.Mutex
	opts Options
	s    *sandbox
}

// sandbox is one seeded demo run.
type sandbox struct {
	home     string
	appDir   string
	vaultDir string

	secrets   *setup.InitSecrets
	sc        *deadswitch.SwitchConfig
	mail      *simenv.MailServer
	tg        *simenv.TelegramServer
	gh        *simenv.GitHubServer
	bareRepo  string
	pkgZip    string
	wfYAML    string
	ghSecrets map[string]string
	engine    string // "cloud" | "local"

	day         int
	released    bool
	releasedDay int
	warnedOnce  bool
	mailSeen    int // high-water mark into mail.Messages()
	pingSeen    int // high-water mark into tg.Pings()

	cron   []CronRun
	mails  []MailView
	phone  []PhonePing
	events []Event

	recipient RecipientPanel
}

// Snapshot is the world as each actor sees it; its JSON tags are the demo API
// contract consumed by the GUI.
type Snapshot struct {
	Day            int            `json:"day"`
	Engine         string         `json:"engine"`
	Stage          string         `json:"stage"`
	Released       bool           `json:"released"`
	ReleasedDay    int            `json:"releasedDay,omitempty"`
	KeyB64         string         `json:"keyB64,omitempty"` // revealed only once released
	CardWords      string         `json:"cardWords"`
	OwnerEmail     string         `json:"ownerEmail"`
	RecipientEmail string         `json:"recipientEmail"`
	Thresholds     Thresholds     `json:"thresholds"`
	Owner          OwnerPanel     `json:"owner"`
	Cloud          CloudPanel     `json:"cloud"`
	OwnerInbox     []MailView     `json:"ownerInbox"`
	RecipientInbox []MailView     `json:"recipientInbox"`
	Phone          []PhonePing    `json:"phone"`
	Recipient      RecipientPanel `json:"recipient"`
	Events         []Event        `json:"events"`
}

type Thresholds struct {
	Warning1 int `json:"warning1"`
	Warning2 int `json:"warning2"`
	Final    int `json:"final"`
}

type OwnerPanel struct {
	LastCheckin string `json:"lastCheckin"`
	DaysSince   int    `json:"daysSince"`
	EntryCount  int    `json:"entryCount"`
	CloudOnly   bool   `json:"cloudOnly"`
}

type CloudPanel struct {
	Repo            string    `json:"repo"`
	Heartbeat       string    `json:"heartbeat"`
	WorkflowPresent bool      `json:"workflowPresent"`
	Cron            []CronRun `json:"cron"`
}

// CronRun is one simulated day of automation.
type CronRun struct {
	Day       int    `json:"day"`
	Engine    string `json:"engine"`
	Status    string `json:"status"`
	DaysSince int    `json:"daysSince"`
	MailCount int    `json:"mailCount"`
}

// MailView is one email as an inbox shows it.
type MailView struct {
	Day     int      `json:"day"`
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Body    string   `json:"body"`
	Release bool     `json:"release"`
	Via     string   `json:"via"` // "cloud" | "local"
}

// PhonePing is one Telegram bot message on the owner's phone.
type PhonePing struct {
	Day  int    `json:"day"`
	Text string `json:"text"`
	Via  string `json:"via"` // "cloud" | "local"
}

// RecipientPanel is what the recipient column shows after a successful open.
type RecipientPanel struct {
	Opened bool     `json:"opened"`
	Files  []string `json:"files,omitempty"`
	Index  string   `json:"index,omitempty"`
}

// Event is one entry of the story log; the SPA translates Code.
type Event struct {
	Day  int    `json:"day"`
	Code string `json:"code"` // armed, packaged, checkin, warned, released, opened, reset
	Arg  string `json:"arg,omitempty"`
}

// NewWorld seeds a fresh sandbox. It REPOINTS THE PROCESS HOME at the sandbox
// (os.Setenv HOME/USERPROFILE): the demo command owns its process, which is what
// isolates a real vault on the machine. Tests must set a throwaway HOME with
// t.Setenv first so cleanup restores it.
func NewWorld(opts Options) (*World, error) {
	s, err := newSandbox(opts)
	if err != nil {
		return nil, err
	}
	return &World{opts: opts, s: s}, nil
}

// Password returns the sandbox owner password (the GUI auto-unlocks with it).
func (w *World) Password() string { return demoPassword }

// Close tears the sandbox down and removes its files.
func (w *World) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.s.close()
}

func (s *sandbox) close() error {
	if s.mail != nil {
		s.mail.Close()
	}
	if s.tg != nil {
		s.tg.Close()
	}
	if s.gh != nil {
		s.gh.Close()
	}
	return os.RemoveAll(s.home)
}

func newSandbox(opts Options) (*sandbox, error) {
	home, err := os.MkdirTemp("", "kawarimi-demo-")
	if err != nil {
		return nil, err
	}
	s := &sandbox{home: home}
	if err := s.seed(opts); err != nil {
		s.close()
		return nil, err
	}
	return s, nil
}

// seed builds the world through the same product calls the real setup uses.
func (s *sandbox) seed(opts Options) error {
	os.Setenv("HOME", s.home)
	os.Setenv("USERPROFILE", s.home)
	s.appDir = filepath.Join(s.home, config.AppDir)
	s.vaultDir = filepath.Join(s.home, "vault")

	s.engine = "cloud"
	if opts.ForceLocalEngine || simenv.WorkflowRunnerSupported() != nil {
		s.engine = "local"
	}

	// The owner creates the vault (fast KDF: this is a sandbox, not an estate).
	fast := crypto.TestParams()
	secrets, err := setup.InitVault(setup.InitOptions{
		VaultDir:          s.vaultDir,
		Password:          demoPassword,
		DeviceID:          "demo-laptop",
		MnemonicKDFParams: &fast,
		OwnerKDFParams:    &fast,
	})
	if err != nil {
		return fmt.Errorf("demo init: %w", err)
	}
	s.secrets = secrets

	if err := s.addSampleEntries(); err != nil {
		return err
	}

	// Mock actors.
	if s.mail, err = simenv.StartMail(); err != nil {
		return err
	}
	if s.tg, err = simenv.StartTelegram(); err != nil {
		return err
	}
	os.Setenv("KAWARIMI_TELEGRAM_API", s.tg.URL())
	if s.gh, err = simenv.StartGitHub(); err != nil {
		return err
	}
	os.Setenv("KAWARIMI_GITHUB_API", s.gh.URL())
	s.bareRepo = filepath.Join(s.home, "cloud-dms.git")
	if err := simenv.InitBareRepo(s.bareRepo); err != nil {
		return err
	}
	s.gh.RepoSSHURL = s.bareRepo

	// The owner configures the switch.
	sc := deadswitch.DefaultSwitchConfig()
	sc.SMTPServer = s.mail.Host
	sc.SMTPPort = s.mail.Port
	sc.SMTPUsername = senderEmail
	sc.SMTPPassword = "demo-smtp-pw"
	sc.SenderEmail = senderEmail
	sc.UserEmail = ownerEmail
	sc.Recipients = []string{recipientEmail}
	sc.VaultPackageLocation = "https://drive.demo.example/kawarimi-vault.zip"
	sc.TelegramBotToken = "demo-bot-token"
	sc.TelegramChatID = demoChatID
	sc.PingChannels = []string{"email", "telegram"}
	s.sc = sc

	// Cloud engine keeps the recommended cloud-only mode; the local engine needs
	// the local-release payload or deadswitch.Evaluate could never finish the story.
	localRelease := s.engine == "local"
	if err := setup.StoreSwitchPayloadForMode(s.appDir, localRelease); err != nil {
		return err
	}
	if err := deadswitch.SaveSwitchConfig(s.appDir, sc); err != nil {
		return err
	}

	// The owner arms the cloud: private repo, Actions secrets, seeded workflow —
	// all through the real client against the mock API and the local bare repo.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client := github.NewClient("ghp_demo")
	repo, err := client.CreatePrivateRepo(ctx, "kawarimi-dms-demo")
	if err != nil {
		return fmt.Errorf("demo cloud repo: %w", err)
	}
	dmsKeyData, err := os.ReadFile(filepath.Join(s.appDir, "dms-key"))
	if err != nil {
		return fmt.Errorf("demo dms key: %w", err)
	}
	ghSecrets := map[string]string{
		"SMTP_SERVER":            sc.SMTPServer,
		"SMTP_USERNAME":          sc.SMTPUsername,
		"SMTP_PASSWORD":          sc.SMTPPassword,
		"USER_EMAIL":             sc.UserEmail,
		"RECIPIENT_EMAILS":       strings.Join(sc.Recipients, ","),
		"VAULT_PACKAGE_LOCATION": sc.VaultPackageLocation,
		"DMS_KEY":                strings.TrimSpace(string(dmsKeyData)),
		"TELEGRAM_BOT_TOKEN":     sc.TelegramBotToken,
		"TELEGRAM_CHAT_ID":       sc.TelegramChatID,
	}
	if err := client.SetActionsSecrets(ctx, repo.Owner, repo.Name, ghSecrets); err != nil {
		return fmt.Errorf("demo cloud secrets: %w", err)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if _, err := setup.SeedSwitch(cfg, sc, repo.SSHURL, false); err != nil {
		return fmt.Errorf("demo seed switch: %w", err)
	}
	if !localRelease {
		os.Remove(filepath.Join(s.appDir, "dms-key")) // cloud-only hygiene, as the wizard does
	}

	// The owner builds the public package.
	s.pkgZip = filepath.Join(s.home, "kawarimi-vault.zip")
	if err := vault.BuildPackage(s.vaultDir, s.pkgZip, ""); err != nil {
		return fmt.Errorf("demo package: %w", err)
	}

	wf, ok, err := simenv.RepoFile(s.bareRepo, ".github/workflows/deadman.yml")
	if err != nil || !ok {
		return fmt.Errorf("demo workflow missing from cloud repo: %v", err)
	}
	s.wfYAML = wf
	s.ghSecrets = s.gh.Secrets()

	// Day 0 begins with a fresh proof of life, like a real just-armed install.
	if err := s.pushBackdatedHeartbeat(); err != nil {
		return fmt.Errorf("demo initial check-in: %w", err)
	}

	s.events = append(s.events,
		Event{Day: 0, Code: "armed"},
		Event{Day: 0, Code: "packaged"},
	)
	return nil
}

// addSampleEntries opens the fresh vault the owner way and stores bilingual demo
// content, so the recipient reveal at the end shows something real.
func (s *sandbox) addSampleEntries() error {
	h, err := vault.LoadHeader(s.vaultDir)
	if err != nil {
		return err
	}
	masterKey, identity, err := h.OpenWithMnemonic(s.secrets.MnemonicWords)
	if err != nil {
		return err
	}
	defer crypto.ZeroBytes(masterKey)
	v, err := vault.OpenV2(s.vaultDir, identity, h.AgeRecipient)
	if err != nil {
		return err
	}
	if _, err := v.AddNote("Instrucciones del banco / Bank instructions",
		[]byte("ES: La cuenta principal está en el Banco Demo, IBAN ES00 0000 0000.\n"+
			"EN: The main account is at Demo Bank, IBAN ES00 0000 0000.\n"), nil); err != nil {
		return err
	}
	if _, err := v.AddNote("Carta de despedida / Farewell letter",
		[]byte("ES: Si estás leyendo esto, gracias por todo. Os quiero.\n"+
			"EN: If you are reading this, thank you for everything. I love you.\n"), nil); err != nil {
		return err
	}
	if _, err := v.AddCredential(&vault.Credential{
		Service:  "Demo Email",
		Username: "owner@demo.example",
		Password: "correct-horse-demo",
		Notes:    "ES: Cuenta de correo principal. EN: Main email account.",
	}, nil); err != nil {
		return err
	}
	return nil
}

func (s *sandbox) targets() deadswitch.CheckinTargets {
	return deadswitch.CheckinTargets{
		VaultDir:   s.vaultDir,
		DMSRepoDir: filepath.Join(s.appDir, config.DMSRepoName),
		DMSRemote:  s.bareRepo,
	}
}

// Advance moves the story forward by days, running each day's automation.
func (w *World) Advance(days int) (*Snapshot, error) {
	if days < 1 || days > 365 {
		return nil, fmt.Errorf("days must be between 1 and 365")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for i := 0; i < days; i++ {
		if err := w.s.advanceOneDay(); err != nil {
			return nil, err
		}
	}
	// Sync the cloud heartbeat ONCE per user action, not once per simulated day:
	// dozens of rapid go-git pushes to the same bare repo occasionally trip a
	// transient "packfile not found" on slow CI filesystems.
	if err := w.s.pushBackdatedHeartbeat(); err != nil {
		return nil, err
	}
	return w.s.snapshot()
}

// pushBackdatedHeartbeat records the current simulated silence through the real
// check-in path (local file + cloud push), retrying a couple of times because
// go-git's local-filesystem transport is flaky under CI disk latency.
func (s *sandbox) pushBackdatedHeartbeat() error {
	when := time.Now().Add(-time.Duration(s.day) * 24 * time.Hour)
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if _, err = deadswitch.RecordCheckin(s.targets(), when); err == nil {
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return fmt.Errorf("syncing cloud heartbeat: %w", err)
}

func (s *sandbox) advanceOneDay() error {
	s.day++

	// Backdate the LOCAL heartbeat directly (same trick the lifecycle tests use);
	// the cloud repo is synced once per Advance, and the cron reads a scratch
	// checkout, so nothing else needs a git push per simulated day.
	ts := time.Now().Add(-time.Duration(s.day) * 24 * time.Hour).UTC().Format(time.RFC3339)
	if err := os.WriteFile(filepath.Join(s.vaultDir, vault.LastCheckinFile), []byte(ts+"\n"), 0600); err != nil {
		return fmt.Errorf("backdating heartbeat: %w", err)
	}
	// The clock-jump ratchet demands real elapsed overdue time before a LOCAL
	// release; give it the anchor the daily timer would have recorded. Re-written
	// every day so a mid-story check-in cannot leave a stale anchor behind.
	anchor := filepath.Join(s.appDir, "first-overdue-at")
	if s.day >= s.sc.Warning1Days {
		ts := time.Now().Add(-time.Duration(s.day-s.sc.Warning1Days) * 24 * time.Hour)
		if err := os.WriteFile(anchor, []byte(ts.UTC().Format(time.RFC3339)+"\n"), 0600); err != nil {
			return err
		}
	} else {
		os.Remove(anchor)
	}

	// The cloud cron: the REAL generated workflow bash, curl shimmed.
	if s.engine == "cloud" {
		if err := s.runCloudCron(); err != nil {
			return err
		}
	}

	// The local daily timer tick (real deadswitch.Evaluate): Telegram /alive
	// auto-check-ins, owner reminders, and — on the local engine — the release.
	preMail := s.mail.Count()
	if err := deadswitch.Evaluate(s.targets(), s.sc, s.appDir); err != nil {
		return fmt.Errorf("local evaluate: %w", err)
	}
	s.collectLocalTraffic()
	s.tg.ClearAlive() // a scripted /alive is consumed by exactly one tick

	// Did the tick auto-check-in (Telegram /alive)? The heartbeat jumps to now.
	if ds, err := deadswitch.DaysSinceCheckin(s.vaultDir); err == nil && ds < s.day {
		s.day = 0
		s.events = append(s.events, Event{Day: s.day, Code: "checkin", Arg: "telegram"})
	}

	if s.engine == "local" {
		s.cron = append(s.cron, CronRun{
			Day: s.day, Engine: "local",
			Status:    "ok",
			DaysSince: s.day,
			MailCount: s.mail.Count() - preMail,
		})
	}

	s.noteWarnedAndReleased()
	return nil
}

// runCloudCron executes the real deadman.yml against a scratch checkout carrying
// the heartbeat as of the simulated day (written directly, as the lifecycle story
// test does — the bare repo itself is synced once per Advance).
func (s *sandbox) runCloudCron() error {
	checkout, err := os.MkdirTemp(s.home, "cron-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(checkout)
	ts := time.Now().Add(-time.Duration(s.day) * 24 * time.Hour).UTC().Format(time.RFC3339)
	if err := os.WriteFile(filepath.Join(checkout, "last_checkin"), []byte(ts+"\n"), 0600); err != nil {
		return err
	}
	res, err := simenv.RunDMSWorkflow(s.wfYAML, s.ghSecrets, checkout)
	if err != nil {
		return fmt.Errorf("cloud cron: %w", err)
	}
	daysSince, _ := strconv.Atoi(res.Outputs["days_since"])
	s.cron = append(s.cron, CronRun{
		Day: s.day, Engine: "cloud",
		Status:    res.Outputs["status"],
		DaysSince: daysSince,
		MailCount: len(res.Mails),
	})
	for _, m := range res.Mails {
		s.mails = append(s.mails, MailView{
			Day: s.day, From: m.From, To: m.To,
			Subject: m.Subject(), Body: m.Body(),
			Release: sentTo(m.To, recipientEmail), Via: "cloud",
		})
	}
	for _, p := range res.Pings {
		s.phone = append(s.phone, PhonePing{Day: s.day, Text: p.Text, Via: "cloud"})
	}
	return nil
}

// collectLocalTraffic folds new SMTP-mock messages and Telegram pings into the views.
func (s *sandbox) collectLocalTraffic() {
	msgs := s.mail.Messages()
	for _, m := range msgs[s.mailSeen:] {
		s.mails = append(s.mails, MailView{
			Day: s.day, From: m.From, To: m.To,
			Subject: m.Subject, Body: m.Body,
			Release: sentTo(m.To, recipientEmail), Via: "local",
		})
	}
	s.mailSeen = len(msgs)

	pings := s.tg.Pings()
	for _, p := range pings[s.pingSeen:] {
		s.phone = append(s.phone, PhonePing{Day: s.day, Text: p, Via: "local"})
	}
	s.pingSeen = len(pings)
}

func (s *sandbox) noteWarnedAndReleased() {
	if !s.warnedOnce {
		for _, m := range s.mails {
			if sentTo(m.To, ownerEmail) {
				s.warnedOnce = true
				s.events = append(s.events, Event{Day: m.Day, Code: "warned"})
				break
			}
		}
	}
	if !s.released {
		for _, m := range s.mails {
			if m.Release {
				s.released = true
				s.releasedDay = m.Day
				s.events = append(s.events, Event{Day: m.Day, Code: "released"})
				break
			}
		}
	}
}

func sentTo(to []string, addr string) bool {
	for _, t := range to {
		if strings.EqualFold(strings.TrimSpace(t), addr) {
			return true
		}
	}
	return false
}

// Checkin is the owner proving life: heartbeat to now, story clock back to day 0.
func (w *World) Checkin() (*Snapshot, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.s.day = 0
	if err := w.s.pushBackdatedHeartbeat(); err != nil {
		return nil, err
	}
	os.Remove(filepath.Join(w.s.appDir, "first-overdue-at"))
	w.s.events = append(w.s.events, Event{Day: 0, Code: "checkin"})
	return w.s.snapshot()
}

// TelegramAlive scripts a "/alive" reply from the owner's phone; the next day's
// local timer tick consumes it and auto-checks-in through the real product path.
func (w *World) TelegramAlive() (*Snapshot, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.s.tg.ScriptAlive(demoChatID)
	return w.s.snapshot()
}

// RecipientOpen plays the recipient on a fresh machine: extract the real package
// zip, unseal with the emailed KEY plus the card WORDS, export the files.
func (w *World) RecipientOpen(keyB64, words string) (*Snapshot, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	s := w.s

	machine := filepath.Join(s.home, "recipient-machine")
	if err := os.MkdirAll(machine, 0700); err != nil {
		return nil, err
	}
	extractDir, err := os.MkdirTemp(machine, "extract-")
	if err != nil {
		return nil, err
	}
	pkgVaultDir, err := vault.ExtractPackage(s.pkgZip, extractDir)
	if err != nil {
		return nil, err
	}
	dmsKey, err := crypto.DecodeDMSKeyLenient(keyB64)
	if err != nil {
		os.RemoveAll(extractDir)
		return nil, fmt.Errorf("that does not look like the key from the email")
	}
	defer crypto.ZeroBytes(dmsKey)
	v, err := vault.OpenSealedV4(pkgVaultDir, dmsKey, words)
	if err != nil {
		os.RemoveAll(extractDir)
		return nil, fmt.Errorf("the key + words do not open the vault (this is the same wall an attacker with only one of the two hits)")
	}
	decrypted := filepath.Join(machine, "decrypted")
	if err := v.Export(decrypted); err != nil {
		return nil, err
	}

	var files []string
	filepath.Walk(decrypted, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			if rel, rerr := filepath.Rel(decrypted, path); rerr == nil {
				files = append(files, rel)
			}
		}
		return nil
	})
	index, _ := os.ReadFile(filepath.Join(decrypted, "INDEX.md"))

	s.recipient = RecipientPanel{Opened: true, Files: files, Index: string(index)}
	s.events = append(s.events, Event{Day: s.day, Code: "opened"})
	return s.snapshot()
}

// Reset discards the sandbox and seeds a brand-new one (fresh secrets, day 0).
func (w *World) Reset() (*Snapshot, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	fresh, err := newSandbox(w.opts)
	if err != nil {
		return nil, err
	}
	old := w.s
	w.s = fresh
	old.close()
	w.s.events = append(w.s.events, Event{Day: 0, Code: "reset"})
	return w.s.snapshot()
}

// Snapshot returns the current world state.
func (w *World) Snapshot() (*Snapshot, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.s.snapshot()
}

func (s *sandbox) snapshot() (*Snapshot, error) {
	// Every list field marshals as a JSON array, never null — the SPA iterates
	// them without per-field guards.
	snap := &Snapshot{
		Day:            s.day,
		Engine:         s.engine,
		Released:       s.released,
		ReleasedDay:    s.releasedDay,
		CardWords:      s.secrets.RecipientPassphrase,
		OwnerEmail:     ownerEmail,
		RecipientEmail: recipientEmail,
		Thresholds: Thresholds{
			Warning1: s.sc.Warning1Days,
			Warning2: s.sc.Warning2Days,
			Final:    s.sc.FinalDays,
		},
		Cloud: CloudPanel{
			Repo: "demo/kawarimi-dms-demo",
			Cron: append([]CronRun{}, s.cron...),
		},
		Phone:          append([]PhonePing{}, s.phone...),
		Recipient:      s.recipient,
		Events:         append([]Event{}, s.events...),
		OwnerInbox:     []MailView{},
		RecipientInbox: []MailView{},
	}
	if s.released {
		snap.KeyB64 = s.ghSecrets["DMS_KEY"]
	}

	stage := deadswitch.EvaluateStage(s.day, s.sc)
	snap.Stage = map[deadswitch.Stage]string{
		deadswitch.StageNormal:   "normal",
		deadswitch.StageWarning1: "warning1",
		deadswitch.StageWarning2: "warning2",
		deadswitch.StageFinal:    "final",
	}[stage]

	if last, err := deadswitch.ReadLastCheckin(s.vaultDir); err == nil {
		snap.Owner.LastCheckin = last.UTC().Format(time.RFC3339)
	}
	snap.Owner.DaysSince = s.day
	snap.Owner.EntryCount = 3
	snap.Owner.CloudOnly = s.engine == "cloud"

	if hb, ok, err := simenv.RepoFile(s.bareRepo, "last_checkin"); err == nil && ok {
		snap.Cloud.Heartbeat = strings.TrimSpace(hb)
	}
	if _, ok, err := simenv.RepoFile(s.bareRepo, ".github/workflows/deadman.yml"); err == nil {
		snap.Cloud.WorkflowPresent = ok
	}

	for _, m := range s.mails {
		if sentTo(m.To, ownerEmail) {
			snap.OwnerInbox = append(snap.OwnerInbox, m)
		}
		if sentTo(m.To, recipientEmail) {
			snap.RecipientInbox = append(snap.RecipientInbox, m)
		}
	}
	return snap, nil
}
