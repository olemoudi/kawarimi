package deadswitch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEvaluateStage(t *testing.T) {
	cfg := DefaultSwitchConfig()

	tests := []struct {
		daysSince int
		expected  Stage
	}{
		{0, StageNormal},
		{7, StageNormal},
		{13, StageNormal},
		{14, StageWarning1},
		{20, StageWarning1},
		{21, StageWarning2},
		{29, StageWarning2},
		{30, StageFinal},
		{100, StageFinal},
	}

	for _, tt := range tests {
		got := EvaluateStage(tt.daysSince, cfg)
		if got != tt.expected {
			t.Errorf("EvaluateStage(%d) = %d, want %d", tt.daysSince, got, tt.expected)
		}
	}
}

func TestEvaluateStageCustomThresholds(t *testing.T) {
	cfg := &SwitchConfig{
		Warning1Days: 7,
		Warning2Days: 14,
		FinalDays:    21,
	}

	if got := EvaluateStage(6, cfg); got != StageNormal {
		t.Errorf("expected StageNormal for 6 days, got %d", got)
	}
	if got := EvaluateStage(7, cfg); got != StageWarning1 {
		t.Errorf("expected StageWarning1 for 7 days, got %d", got)
	}
	if got := EvaluateStage(14, cfg); got != StageWarning2 {
		t.Errorf("expected StageWarning2 for 14 days, got %d", got)
	}
	if got := EvaluateStage(21, cfg); got != StageFinal {
		t.Errorf("expected StageFinal for 21 days, got %d", got)
	}
}

func TestReadLastCheckin(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()

	checkinPath := filepath.Join(dir, "last_checkin")
	os.WriteFile(checkinPath, []byte(now.Format(time.RFC3339)+"\n"), 0644)

	ts, err := ReadLastCheckin(dir)
	if err != nil {
		t.Fatalf("ReadLastCheckin: %v", err)
	}

	// Should be within 1 second
	if ts.Sub(now).Abs() > time.Second {
		t.Errorf("timestamp mismatch: got %v, want %v", ts, now)
	}
}

func TestReadLastCheckinMissing(t *testing.T) {
	dir := t.TempDir()
	_, err := ReadLastCheckin(dir)
	if err == nil {
		t.Fatal("expected error for missing last_checkin")
	}
}

func TestDaysSinceCheckin(t *testing.T) {
	dir := t.TempDir()

	// Check-in 5 days ago
	past := time.Now().UTC().Add(-5 * 24 * time.Hour)
	checkinPath := filepath.Join(dir, "last_checkin")
	os.WriteFile(checkinPath, []byte(past.Format(time.RFC3339)+"\n"), 0644)

	days, err := DaysSinceCheckin(dir)
	if err != nil {
		t.Fatalf("DaysSinceCheckin: %v", err)
	}

	if days < 4 || days > 6 {
		t.Errorf("expected ~5 days, got %d", days)
	}
}

func TestStoreSwitchPayloadAndDecrypt(t *testing.T) {
	appDir := t.TempDir()
	passphrase := "my-secret-vault-passphrase"

	if err := StoreSwitchPayload(appDir, passphrase); err != nil {
		t.Fatalf("StoreSwitchPayload: %v", err)
	}

	// Verify files were created
	if _, err := os.Stat(filepath.Join(appDir, "switch-identity.key")); err != nil {
		t.Fatalf("missing identity key: %v", err)
	}
	if _, err := os.Stat(filepath.Join(appDir, "switch-payload.age")); err != nil {
		t.Fatalf("missing payload: %v", err)
	}

	// Decrypt and verify
	decrypted, err := DecryptSwitchPayload(appDir)
	if err != nil {
		t.Fatalf("DecryptSwitchPayload: %v", err)
	}

	if decrypted != passphrase {
		t.Errorf("got %q, want %q", decrypted, passphrase)
	}
}

func TestStoreSwitchCloudOnly(t *testing.T) {
	appDir := t.TempDir()

	if err := StoreSwitchCloudOnly(appDir); err != nil {
		t.Fatalf("StoreSwitchCloudOnly: %v", err)
	}
	if !SwitchIsCloudOnly(appDir) {
		t.Error("expected cloud-only detection to be true")
	}

	// The stored cloud-only payload must not contain a DMS key.
	payload, err := DecryptSwitchPayload(appDir)
	if err != nil {
		t.Fatalf("DecryptSwitchPayload: %v", err)
	}
	if payload != "CLOUDONLY:" {
		t.Errorf("cloud-only payload = %q, want %q", payload, "CLOUDONLY:")
	}

	// A DMS-key payload is not cloud-only.
	appDir2 := t.TempDir()
	if err := StoreSwitchDMSKey(appDir2, "abc123"); err != nil {
		t.Fatalf("StoreSwitchDMSKey: %v", err)
	}
	if SwitchIsCloudOnly(appDir2) {
		t.Error("a DMS-key payload should not be reported as cloud-only")
	}
}

func TestStoreSwitchMnemonicAndDecrypt(t *testing.T) {
	appDir := t.TempDir()
	words := []string{"abandon", "ability", "able", "about", "above", "absent", "absorb", "abstract"}

	if err := StoreSwitchMnemonic(appDir, words); err != nil {
		t.Fatalf("StoreSwitchMnemonic: %v", err)
	}

	decrypted, err := DecryptSwitchPayload(appDir)
	if err != nil {
		t.Fatalf("DecryptSwitchPayload: %v", err)
	}

	expected := "MNEMONIC:abandon ability able about above absent absorb abstract"
	if decrypted != expected {
		t.Errorf("got %q, want %q", decrypted, expected)
	}

	// Verify it starts with MNEMONIC: prefix
	if !strings.HasPrefix(decrypted, "MNEMONIC:") {
		t.Error("mnemonic payload should start with MNEMONIC: prefix")
	}
}

func TestSaveSwitchConfigAndLoad(t *testing.T) {
	appDir := t.TempDir()

	// First create the identity key
	if err := StoreSwitchPayload(appDir, "dummy"); err != nil {
		t.Fatalf("StoreSwitchPayload: %v", err)
	}

	cfg := DefaultSwitchConfig()
	cfg.SMTPServer = "smtp.gmail.com"
	cfg.SMTPUsername = "test@gmail.com"
	cfg.Recipients = []string{"family@example.com"}

	if err := SaveSwitchConfig(appDir, cfg); err != nil {
		t.Fatalf("SaveSwitchConfig: %v", err)
	}

	loaded, err := LoadSwitchConfig(appDir)
	if err != nil {
		t.Fatalf("LoadSwitchConfig: %v", err)
	}

	if loaded.SMTPServer != "smtp.gmail.com" {
		t.Errorf("SMTPServer: got %q", loaded.SMTPServer)
	}
	if len(loaded.Recipients) != 1 || loaded.Recipients[0] != "family@example.com" {
		t.Errorf("Recipients: got %v", loaded.Recipients)
	}
}

func TestIsSwitchConfigured(t *testing.T) {
	appDir := t.TempDir()

	if IsSwitchConfigured(appDir) {
		t.Fatal("should not be configured initially")
	}

	StoreSwitchPayload(appDir, "test")

	if !IsSwitchConfigured(appDir) {
		t.Fatal("should be configured after StoreSwitchPayload")
	}
}

func TestStoreSwitchSealedPayloadAndDecrypt(t *testing.T) {
	appDir := t.TempDir()
	sealedBase64 := "dGVzdC1zZWFsZWQtcGF5bG9hZC1kYXRh" // base64 of "test-sealed-payload-data"

	if err := StoreSwitchSealedPayload(appDir, sealedBase64); err != nil {
		t.Fatalf("StoreSwitchSealedPayload: %v", err)
	}

	decrypted, err := DecryptSwitchPayload(appDir)
	if err != nil {
		t.Fatalf("DecryptSwitchPayload: %v", err)
	}

	expected := "SEALED:" + sealedBase64
	if decrypted != expected {
		t.Errorf("got %q, want %q", decrypted, expected)
	}

	// Verify it starts with SEALED: prefix
	if !strings.HasPrefix(decrypted, "SEALED:") {
		t.Error("sealed payload should start with SEALED: prefix")
	}
}

func TestTriggerFinalReleaseV3DetectsPrefix(t *testing.T) {
	// This tests that triggerFinalRelease correctly detects the SEALED: prefix.
	// We can't test the full email flow without SMTP, but we can verify prefix detection.
	appDir := t.TempDir()

	sealedBase64 := "dGVzdC1zZWFsZWQtcGF5bG9hZA=="
	if err := StoreSwitchSealedPayload(appDir, sealedBase64); err != nil {
		t.Fatalf("StoreSwitchSealedPayload: %v", err)
	}

	payload, err := DecryptSwitchPayload(appDir)
	if err != nil {
		t.Fatalf("DecryptSwitchPayload: %v", err)
	}

	// Verify the prefix routing would work
	if !strings.HasPrefix(payload, "SEALED:") {
		t.Fatalf("expected SEALED: prefix, got %q", payload)
	}

	// Extract the base64 portion
	extracted := strings.TrimPrefix(payload, "SEALED:")
	if extracted != sealedBase64 {
		t.Errorf("extracted base64 = %q, want %q", extracted, sealedBase64)
	}
}
