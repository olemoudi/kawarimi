package deadswitch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateServiceUnit(t *testing.T) {
	content := GenerateServiceUnit("/usr/local/bin/kawarimi")

	if !strings.Contains(content, "ExecStart=/usr/local/bin/kawarimi switch evaluate") {
		t.Error("service unit should contain ExecStart with kawarimi binary")
	}
	if !strings.Contains(content, "Type=oneshot") {
		t.Error("service unit should be Type=oneshot")
	}
}

func TestInstallSystemdUnits(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome) // os.UserHomeDir on Windows

	if err := InstallSystemdUnits("/usr/local/bin/kawarimi"); err != nil {
		t.Fatalf("InstallSystemdUnits: %v", err)
	}

	systemdDir := filepath.Join(tmpHome, ".config", "systemd", "user")

	timerPath := filepath.Join(systemdDir, "kawarimi-switch.timer")
	if _, err := os.Stat(timerPath); err != nil {
		t.Fatalf("timer file missing: %v", err)
	}

	servicePath := filepath.Join(systemdDir, "kawarimi-switch.service")
	if _, err := os.Stat(servicePath); err != nil {
		t.Fatalf("service file missing: %v", err)
	}

	timerContent, _ := os.ReadFile(timerPath)
	if !strings.Contains(string(timerContent), "Persistent=true") {
		t.Error("timer should have Persistent=true")
	}

	serviceContent, _ := os.ReadFile(servicePath)
	if !strings.Contains(string(serviceContent), "switch evaluate") {
		t.Error("service should run 'switch evaluate'")
	}
}
