package gui

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/olemoudi/kawarimi/internal/crypto"
	"github.com/olemoudi/kawarimi/internal/testenv"
	"github.com/olemoudi/kawarimi/internal/vault"
)

// TestSwitchCloudAutomation drives the real /api/switch/cloud endpoint against the
// mock GitHub API and a local bare DMS repo: it must create the private repo, set
// the Actions secrets, push the workflow + heartbeat, and — crucially — the DMS_KEY
// it uploads must actually open the vault with the recipient card passphrase.
func TestSwitchCloudAutomation(t *testing.T) {
	env := testenv.New(t)
	secrets := env.InitVault(t)
	mail := testenv.StartMail(t)
	gh := testenv.StartGitHub(t)
	bare := testenv.BareRepo(t)
	gh.RepoSSHURL = bare // SeedSwitch pushes here (local remote, no SSH)

	// The wizard's switch step must have run first (cloud-only).
	sc := env.SwitchConfig(mail, "owner@test", "heir@test")
	env.ArmSwitch(t, sc, false)

	// An unlocked GUI server.
	s := &server{
		token: testToken, addr: "127.0.0.1:9999", port: "9999",
		opts: Options{Version: "test"}, sess: &session{}, lastSeen: time.Now(), quit: make(chan struct{}),
	}
	if err := s.sess.unlock(env.Password()); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	h := s.routes()

	rec := call(h, "POST", "/api/switch/cloud", map[string]any{
		"githubToken": "ghp_testtoken", "repoName": "kawarimi-dms",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("cloud endpoint: %d (%s)", rec.Code, rec.Body.String())
	}

	// The private repo was created.
	if repos := gh.ReposCreated(); len(repos) != 1 || repos[0] != "kawarimi-dms" {
		t.Fatalf("repos created = %v, want [kawarimi-dms]", repos)
	}

	// The DMS_KEY secret was set and genuinely opens the vault with the card.
	dmsB64, ok := gh.Secret("DMS_KEY")
	if !ok {
		t.Fatal("DMS_KEY secret was not set")
	}
	dmsKey, err := crypto.DecodeDMSKey(dmsB64)
	if err != nil {
		t.Fatalf("decode uploaded DMS_KEY: %v", err)
	}
	if _, err := vault.OpenSealedV4(env.VaultDir, dmsKey, secrets.RecipientPassphrase); err != nil {
		t.Fatalf("the DMS_KEY the cloud would deliver does not open the vault: %v", err)
	}

	// The rest of the required secrets are present.
	for _, name := range []string{"SMTP_SERVER", "SMTP_USERNAME", "SMTP_PASSWORD", "USER_EMAIL", "RECIPIENT_EMAILS", "VAULT_PACKAGE_LOCATION"} {
		if _, ok := gh.Secret(name); !ok {
			t.Errorf("secret %s was not set", name)
		}
	}
	if v, _ := gh.Secret("RECIPIENT_EMAILS"); v != "heir@test" {
		t.Errorf("RECIPIENT_EMAILS = %q, want heir@test", v)
	}

	// The workflow + heartbeat were pushed to the DMS repo.
	if !testenv.RepoHasFile(t, bare, "last_checkin") {
		t.Error("heartbeat was not pushed to the DMS repo")
	}
	if !testenv.RepoHasFile(t, bare, ".github/workflows/deadman.yml") {
		t.Error("workflow was not pushed to the DMS repo")
	}

	// Cloud-only: the local DMS key is dropped once the secret is set in GitHub.
	if _, err := os.Stat(filepath.Join(env.AppDir, "dms-key")); err == nil {
		t.Error("local dms-key should be removed in cloud-only mode after cloud setup")
	}
}
