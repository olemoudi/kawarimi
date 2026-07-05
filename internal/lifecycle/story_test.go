package lifecycle

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/olemoudi/kawarimi/internal/deadswitch"
	"github.com/olemoudi/kawarimi/internal/github"
	"github.com/olemoudi/kawarimi/internal/recipient"
	"github.com/olemoudi/kawarimi/internal/setup"
	"github.com/olemoudi/kawarimi/internal/testenv"
	"github.com/olemoudi/kawarimi/internal/vault"
)

// TestStory_OwnerDiesRecipientOpensVault is the full role-play of the product,
// through the artifacts real people touch — not internal APIs:
//
//	OWNER    creates the vault, writes a note, arms the cloud switch (mock GitHub),
//	         builds the public package, hands the heir a printed card, checks in,
//	         then goes silent.
//	CRON     the *actual generated workflow bash* runs at day 2 (quiet), day 15
//	         (warns the owner only), day 31 (emails the DMS key to the heir), and
//	         with a broken heartbeat (fail-closed alert to the owner).
//	ATTACKER with the public package + the card (no key), and with the key but a
//	         wrong card, gets nothing.
//	HEIR     on a fresh machine, opens the package with the recipient wizard using
//	         only the zip, the key pasted from the captured email, and the card.
func TestStory_OwnerDiesRecipientOpensVault(t *testing.T) {
	testenv.RequireWorkflowRunner(t)

	// ---------- OWNER: create the vault and write a farewell note ----------
	env := testenv.New(t)
	secrets := env.InitVault(t)

	const farewell = "The safe code is 1234. The notary is Ms. Vega. Te quiero."
	v := env.OwnerVault(t)
	if _, err := v.AddNote("Bank instructions", []byte(farewell), nil); err != nil {
		t.Fatalf("owner adds note: %v", err)
	}

	// The physical card handed to the heir, and the owner's paper mnemonic.
	card := secrets.RecipientPassphrase
	mnemonic := strings.Join(secrets.MnemonicWords, " ")

	// ---------- OWNER: configure + arm the cloud switch ----------
	mail := testenv.StartMail(t)
	sc := env.SwitchConfig(mail, "owner@test", "heir@test")
	env.ArmSwitch(t, sc, false) // cloud-only: this machine keeps no DMS key

	gh := testenv.StartGitHub(t)
	bare := testenv.BareRepo(t)
	gh.RepoSSHURL = bare

	dmsKeyB64, err := os.ReadFile(filepath.Join(env.AppDir, "dms-key"))
	if err != nil {
		t.Fatalf("dms-key missing after init: %v", err)
	}

	ctx := context.Background()
	client := github.NewClient("ghp_story")
	repo, err := client.CreatePrivateRepo(ctx, "kawarimi-dms")
	if err != nil {
		t.Fatalf("create DMS repo: %v", err)
	}
	if err := client.SetActionsSecrets(ctx, repo.Owner, repo.Name, map[string]string{
		"SMTP_SERVER":            sc.SMTPServer,
		"SMTP_USERNAME":          sc.SMTPUsername,
		"SMTP_PASSWORD":          sc.SMTPPassword,
		"USER_EMAIL":             sc.UserEmail,
		"RECIPIENT_EMAILS":       strings.Join(sc.Recipients, ","),
		"VAULT_PACKAGE_LOCATION": sc.VaultPackageLocation,
		"DMS_KEY":                strings.TrimSpace(string(dmsKeyB64)),
	}); err != nil {
		t.Fatalf("set Actions secrets: %v", err)
	}
	if _, err := setup.SeedSwitch(env.Config(t), sc, repo.SSHURL, false); err != nil {
		t.Fatalf("seed switch: %v", err)
	}
	// Cloud-only hygiene: with the secret in GitHub, the local copy is dropped.
	if err := os.Remove(filepath.Join(env.AppDir, "dms-key")); err != nil {
		t.Fatalf("drop local dms-key: %v", err)
	}

	// ---------- OWNER: build the public package and "upload" it ----------
	pkgZip := filepath.Join(t.TempDir(), "kawarimi-vault.zip")
	if err := vault.BuildPackage(env.VaultDir, pkgZip, ""); err != nil {
		t.Fatalf("build package: %v", err)
	}

	// The workflow + heartbeat the owner's setup pushed to the cloud repo.
	wfYAML, ok := testenv.RepoFile(t, bare, ".github/workflows/deadman.yml")
	if !ok {
		t.Fatal("workflow was not pushed to the DMS repo")
	}
	ghSecrets := gh.Secrets()

	// cronAt simulates one scheduled run against a heartbeat that is `days` old.
	cronAt := func(days int) *testenv.WorkflowResult {
		repoDir := t.TempDir()
		hb := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour).Format(time.RFC3339)
		if err := os.WriteFile(filepath.Join(repoDir, "last_checkin"), []byte(hb+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
		return testenv.RunDMSWorkflow(t, wfYAML, ghSecrets, repoDir)
	}

	// ---------- CRON, day 2: owner alive and current — total silence ----------
	if run := cronAt(2); len(run.Mails) != 0 {
		t.Fatalf("day 2: workflow must send nothing, sent %d mail(s): %v", len(run.Mails), run.StepsRun)
	}

	// ---------- CRON, day 15: warn the owner — and ONLY the owner ----------
	run := cronAt(15)
	if len(run.Mails) != 1 || len(run.MailsTo("owner@test")) != 1 {
		t.Fatalf("day 15: want exactly one mail to the owner, got %+v", run.Mails)
	}
	if got := run.MailsTo("heir@test"); len(got) != 0 {
		t.Fatal("day 15: a warning must never reach the recipients")
	}
	if !strings.Contains(run.Mails[0].Subject(), "overdue") {
		t.Errorf("day 15 subject = %q", run.Mails[0].Subject())
	}

	// ---------- CRON, broken heartbeat: fail closed, alert the owner ----------
	brokenDir := t.TempDir() // no last_checkin at all
	run = testenv.RunDMSWorkflow(t, wfYAML, ghSecrets, brokenDir)
	if len(run.Mails) != 1 || len(run.MailsTo("owner@test")) != 1 {
		t.Fatalf("missing heartbeat: want one owner alert, got %+v", run.Mails)
	}
	if !strings.Contains(run.Mails[0].Subject(), "NOT ARMED") || len(run.MailsTo("heir@test")) != 0 {
		t.Fatalf("missing heartbeat must alert the owner only: %q", run.Mails[0].Subject())
	}

	// ---------- OWNER goes silent. CRON, day 31: the release ----------
	// The real final heartbeat: the owner's last check-in, pushed to the cloud.
	if _, err := deadswitch.RecordCheckin(env.CheckinTargets(bare), time.Now().Add(-31*24*time.Hour)); err != nil {
		t.Fatalf("final check-in: %v", err)
	}
	heartbeat, ok := testenv.RepoFile(t, bare, "last_checkin")
	if !ok {
		t.Fatal("heartbeat missing from the DMS repo")
	}
	releaseDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(releaseDir, "last_checkin"), []byte(heartbeat), 0644); err != nil {
		t.Fatal(err)
	}
	run = testenv.RunDMSWorkflow(t, wfYAML, ghSecrets, releaseDir)

	if run.Outputs["status"] != "ok" {
		t.Fatalf("release run: heartbeat status = %q", run.Outputs["status"])
	}
	heirMails := run.MailsTo("heir@test")
	if len(run.Mails) != 1 || len(heirMails) != 1 {
		t.Fatalf("day 31: want exactly the release mail to the heir, got %+v", run.Mails)
	}
	release := heirMails[0]
	body := release.Body()
	if !strings.Contains(body, sc.VaultPackageLocation) {
		t.Error("release mail must tell the heir where to download the package")
	}
	// The V4 security property, asserted on the real artifact: the release email
	// carries ONLY the DMS key — never the card words, never the mnemonic.
	if strings.Contains(body, card) || strings.Contains(body, mnemonic) {
		t.Fatal("release mail leaked the card passphrase or the mnemonic")
	}
	// A grieving non-technical heir must learn about the physical card BEFORE the
	// numbered steps — not discover it mid-procedure at step 5.
	if cardAt := strings.Index(body, "CARD"); cardAt < 0 || cardAt > strings.Index(body, "1. Download") {
		t.Error("release mail must explain the physical card before the steps")
	}

	// The heir follows the instructions: "when it asks for the KEY, paste this".
	key := keyFromReleaseEmail(t, body)
	if key != ghSecrets["DMS_KEY"] {
		t.Fatalf("key in the email = %q, want the DMS_KEY secret", key)
	}

	// ---------- RECIPIENT: a fresh machine, only the zip + email + card ----------
	recipHome := t.TempDir()
	testenv.SetHome(t, recipHome) // no owner state from here on
	downloads := filepath.Join(recipHome, "Downloads")
	if err := os.MkdirAll(downloads, 0755); err != nil {
		t.Fatal(err)
	}
	zipBytes, err := os.ReadFile(pkgZip)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(downloads, "kawarimi-vault.zip"), zipBytes, 0644); err != nil {
		t.Fatal(err)
	}
	decryptedDir := filepath.Join(downloads, "decrypted")

	// ATTACKER 1: has the public package and stole the card — but no key.
	garbageKeys := strings.Repeat("this-is-not-the-key\n", 5)
	if err := runWizard(t, downloads, "2\n"+garbageKeys); err == nil {
		t.Fatal("package + card without the DMS key must not open")
	}
	// ATTACKER 2: intercepted the key and has the package — but no card.
	wrongCard := strings.Repeat(key+"\nwrong words entirely six here\n", 5)
	if err := runWizard(t, downloads, "2\n"+wrongCard); err == nil {
		t.Fatal("package + key without the card must not open")
	}
	if _, err := os.Stat(decryptedDir); !os.IsNotExist(err) {
		t.Fatal("failed attempts must not leave decrypted output behind")
	}

	// THE HEIR: key from the email, words from the card. Spanish-first wizard,
	// they pick English (2), paste the key, type the words.
	if err := runWizard(t, downloads, "2\n"+key+"\n"+card+"\n"); err != nil {
		t.Fatalf("the heir could not open the vault: %v", err)
	}

	index, err := os.ReadFile(filepath.Join(decryptedDir, "INDEX.md"))
	if err != nil {
		t.Fatalf("no INDEX.md after decryption: %v", err)
	}
	if !strings.Contains(string(index), "Bank instructions") {
		t.Errorf("INDEX.md does not list the owner's note:\n%s", index)
	}
	if !decryptedTreeContains(t, decryptedDir, farewell) {
		t.Error("the decrypted files do not contain the owner's note content")
	}
}

// keyFromReleaseEmail extracts the DMS key the way a human reads the email: the
// non-empty line after the English "paste this text:" instruction.
func keyFromReleaseEmail(t *testing.T, body string) string {
	t.Helper()
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if strings.Contains(line, "paste this text:") {
			for _, next := range lines[i+1:] {
				if s := strings.TrimSpace(next); s != "" {
					return s
				}
			}
		}
	}
	t.Fatal("release email has no 'paste this text:' instruction")
	return ""
}

// runWizard drives the real recipient wizard with scripted keystrokes.
func runWizard(t *testing.T, startDir, script string) error {
	t.Helper()
	var out bytes.Buffer
	err := recipient.Run(recipient.Options{
		In:       strings.NewReader(script),
		Out:      &out,
		StartDir: startDir,
	})
	t.Logf("wizard output:\n%s", out.String())
	return err
}

// decryptedTreeContains reports whether any decrypted file contains needle.
func decryptedTreeContains(t *testing.T, root, needle string) bool {
	t.Helper()
	found := false
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || found {
			return err
		}
		data, rerr := os.ReadFile(path)
		if rerr == nil && strings.Contains(string(data), needle) {
			found = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking decrypted output: %v", err)
	}
	return found
}
