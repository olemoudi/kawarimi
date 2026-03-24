package deadswitch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
