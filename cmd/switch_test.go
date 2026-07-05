package cmd

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/olemoudi/kawarimi/internal/config"
	"github.com/olemoudi/kawarimi/internal/deadswitch"
	"github.com/olemoudi/kawarimi/internal/testenv"
)

func initBareDMSRemote(t *testing.T) string {
	t.Helper()
	remote := filepath.Join(t.TempDir(), "dms.git")
	if _, err := git.PlainInitWithOptions(remote, &git.PlainInitOptions{
		InitOptions: git.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName("main")},
		Bare:        true,
	}); err != nil {
		t.Fatalf("init bare remote: %v", err)
	}
	return remote
}

// TestRunSwitchSeedPushesWorkflowAndHeartbeat exercises the full seed path:
// config -> generate workflow -> commit -> push to the (local) DMS remote. It is
// the automated counterpart to the manual GitHub smoke test.
func TestRunSwitchSeedPushesWorkflowAndHeartbeat(t *testing.T) {
	home := testenv.SetHome(t, t.TempDir())

	remote := initBareDMSRemote(t)

	cfg := config.DefaultConfig(filepath.Join(home, "vault"))
	cfg.SyncTargets.DMSRemote = remote // preset so runSwitchSeed does not prompt
	switchCfg := deadswitch.DefaultSwitchConfig()

	reader := bufio.NewReader(strings.NewReader(""))
	if err := runSwitchSeed(reader, cfg, switchCfg, false); err != nil {
		t.Fatalf("runSwitchSeed: %v", err)
	}

	clone := t.TempDir()
	if _, err := git.PlainClone(clone, false, &git.CloneOptions{URL: remote}); err != nil {
		t.Fatalf("clone DMS remote: %v", err)
	}

	workflow := filepath.Join(clone, ".github", "workflows", "deadman.yml")
	data, err := os.ReadFile(workflow)
	if err != nil {
		t.Fatalf("workflow not pushed to DMS remote: %v", err)
	}
	if !strings.Contains(string(data), "Dead Man's Switch") {
		t.Error("pushed workflow missing expected content")
	}

	if _, err := os.Stat(filepath.Join(clone, "last_checkin")); err != nil {
		t.Errorf("heartbeat (last_checkin) not pushed to DMS remote: %v", err)
	}
}

// offerDeleteLocalDMSKey removes the plaintext DMS key only on an explicit "y".
func TestOfferDeleteLocalDMSKey(t *testing.T) {
	env := testenv.New(t)
	if err := os.MkdirAll(env.AppDir, 0700); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(env.AppDir, "dms-key")
	writeKey := func() {
		if err := os.WriteFile(keyPath, []byte("base64key\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	writeKey()
	offerDeleteLocalDMSKey(bufio.NewReader(strings.NewReader("n\n")), env.AppDir)
	if _, err := os.Stat(keyPath); err != nil {
		t.Fatal("answering 'n' must keep the local DMS key")
	}

	offerDeleteLocalDMSKey(bufio.NewReader(strings.NewReader("y\n")), env.AppDir)
	if _, err := os.Stat(keyPath); !os.IsNotExist(err) {
		t.Fatal("answering 'y' must remove the local DMS key")
	}

	// With no key present it must not prompt (a read here would hang forever).
	offerDeleteLocalDMSKey(bufio.NewReader(strings.NewReader("")), env.AppDir)
}

func TestReadLineTrims(t *testing.T) {
	if got := readLine(bufio.NewReader(strings.NewReader("  hello world \r\n"))); got != "hello world" {
		t.Errorf("readLine = %q", got)
	}
}
