package deadswitch

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update", false, "update golden files")

func TestGenerateGitHubWorkflow(t *testing.T) {
	cfg := DefaultSwitchConfig()
	yaml := GenerateGitHubWorkflow(cfg)

	// Check essential elements are present
	checks := []string{
		"name: Dead Man's Switch",
		"schedule:",
		"cron:",
		"last_checkin",
		"days_since",
		"SMTP_SERVER",
		"RECIPIENT_EMAILS",
		"PHYSICAL_PASSPHRASE_LOCATION",
		"DECRYPT_INSTRUCTIONS.md",
		"action-send-mail",
	}

	for _, check := range checks {
		if !strings.Contains(yaml, check) {
			t.Errorf("workflow YAML missing %q", check)
		}
	}
}

func TestInstallGitHubWorkflow(t *testing.T) {
	vaultDir := t.TempDir()
	cfg := DefaultSwitchConfig()

	if err := InstallGitHubWorkflow(vaultDir, cfg); err != nil {
		t.Fatalf("InstallGitHubWorkflow: %v", err)
	}

	workflowPath := filepath.Join(vaultDir, ".github", "workflows", "deadman.yml")
	if _, err := os.Stat(workflowPath); err != nil {
		t.Fatalf("workflow file missing: %v", err)
	}

	content, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("reading workflow: %v", err)
	}

	if !strings.Contains(string(content), "Dead Man's Switch") {
		t.Error("workflow file should contain 'Dead Man's Switch'")
	}
}

func TestGenerateGitHubDMSWorkflowGolden(t *testing.T) {
	cases := []struct {
		name   string
		golden string
		mutate func(*SwitchConfig)
	}{
		{name: "default", golden: "deadman_v4_default.golden.yml"},
		{name: "telegram", golden: "deadman_v4_telegram.golden.yml", mutate: func(c *SwitchConfig) { c.TelegramBotToken = "test-token" }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultSwitchConfig()
			if tc.mutate != nil {
				tc.mutate(cfg)
			}
			got := GenerateGitHubDMSWorkflow(cfg)

			goldenPath := filepath.Join("testdata", tc.golden)
			if *updateGolden {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("reading golden (run `go test -run Golden -update` to create): %v", err)
			}
			if got != string(want) {
				t.Errorf("workflow YAML does not match %s (run with -update to refresh)\n--- got ---\n%s", tc.golden, got)
			}
		})
	}
}

// TestGenerateGitHubDMSWorkflowInvariants locks properties that must hold regardless of
// wording changes (golden files get refreshed; these must not). This is the guard that
// would have caught the %-over-escaping bug (D2) and the fail-open days_since=999 sentinel.
func TestGenerateGitHubDMSWorkflowInvariants(t *testing.T) {
	cfg := DefaultSwitchConfig() // Warning1Days=14, FinalDays=30
	yaml := GenerateGitHubDMSWorkflow(cfg)

	mustContain := []string{
		"date +%s",           // the D2 bug rendered this as `date +%%s`
		"status=missing",     // fail-closed branch present
		"status=unparseable", // fail-closed branch present
		"Alert owner of DMS misconfiguration",
		"permissions:", // least privilege
		"actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683",
		"# kawarimi-dms-workflow-version: 3",    // version marker for drift detection
		"curl --silent --show-error --ssl-reqd", // email sent via curl, no third-party action
		"smtp://$SMTP_SERVER:587",               // templated scheme + default port (was hardcoded 587)
		"--mail-rcpt",
		"days_since >= 14",       // Warning1Days rendered
		"days_since >= 30",       // FinalDays rendered (release threshold)
		"${{ secrets.DMS_KEY }}", // GitHub expression passed through intact
		"ESPANOL",                // release email is bilingual
		"ENGLISH",
		"INDEX.md", // shared canonical step marker across all recipient surfaces
		"tarjeta",  // the physical card is explained to the recipient (both languages)
		"CARD",
	}
	for _, s := range mustContain {
		if !strings.Contains(yaml, s) {
			t.Errorf("workflow YAML missing %q", s)
		}
	}

	mustNotContain := []string{
		"%%",             // leftover Go/printf escaping
		"%!",             // fmt error markers (e.g. %!d(...))
		"days_since=999", // old fail-open sentinel that emailed recipients immediately
		"date -j",        // macOS fallback removed (runner is always ubuntu)
		"age -d",         // recipients must never be told to use the age CLI (fails on V2/V4)
		"dawidd6",        // no third-party mail action (longevity)
		"action-send-mail",
		"server_port: 587", // the port is now templated, not hardcoded
	}
	for _, s := range mustNotContain {
		if strings.Contains(yaml, s) {
			t.Errorf("workflow YAML should not contain %q", s)
		}
	}

	// The ONLY third-party dependency the post-mortem release may have is
	// GitHub-owned actions/checkout — nothing else that could vanish over the years.
	if n := strings.Count(yaml, "uses:"); n != 1 {
		t.Errorf("expected exactly one `uses:` (actions/checkout), found %d", n)
	}

	// Default config has no Telegram token -> no Telegram step.
	if strings.Contains(yaml, "Telegram alert to owner") {
		t.Error("default workflow should not include the Telegram step")
	}
}

func TestGenerateGitHubDMSWorkflowTelegram(t *testing.T) {
	cfg := DefaultSwitchConfig()
	cfg.TelegramBotToken = "test-token"
	yaml := GenerateGitHubDMSWorkflow(cfg)

	if !strings.Contains(yaml, "Telegram alert to owner") {
		t.Error("workflow with a Telegram token should include the Telegram step")
	}
	if !strings.Contains(yaml, "${{ secrets.TELEGRAM_BOT_TOKEN }}") {
		t.Error("Telegram step should reference the bot token secret")
	}
}
