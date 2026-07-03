package copytext

import (
	"strings"
	"testing"
)

func TestPackageInstructionsBilingualAndCorrect(t *testing.T) {
	binaries := []string{"kawarimi-linux-amd64", "kawarimi-darwin-arm64", "kawarimi-windows-amd64.exe"}
	got := PackageInstructions(binaries, "2026-07-03")

	for _, want := range []string{"ESPAÑOL", "ENGLISH", "INDEX.md", "2026-07-03"} {
		if !strings.Contains(got, want) {
			t.Errorf("PackageInstructions missing %q", want)
		}
	}
	// Lists each bundled binary with a friendly label.
	for _, b := range binaries {
		if !strings.Contains(got, b) {
			t.Errorf("PackageInstructions should list binary %q", b)
		}
	}
	if !strings.Contains(got, "Apple Silicon") {
		t.Error("darwin-arm64 should be labelled for Apple Silicon")
	}
	// Must never point recipients at the age CLI (cannot open a V2/V4 vault).
	if strings.Contains(got, "age -d") {
		t.Error("PackageInstructions must not mention `age -d`")
	}
}

func TestReleaseEmailBodyContainsKeyAndLocation(t *testing.T) {
	body := ReleaseEmailBody("https://drive.example/vault.zip", "BASE64KEYVALUE")

	for _, want := range []string{"https://drive.example/vault.zip", "BASE64KEYVALUE", "ESPAÑOL", "ENGLISH", "INDEX.md"} {
		if !strings.Contains(body, want) {
			t.Errorf("ReleaseEmailBody missing %q", want)
		}
	}
	if strings.Contains(body, "age -d") {
		t.Error("ReleaseEmailBody must not mention `age -d`")
	}
}

func TestVaultDocsNoAgeCLI(t *testing.T) {
	if strings.Contains(VaultReadme(), "age -d") {
		t.Error("VaultReadme must not mention `age -d`")
	}
	if strings.Contains(VaultDecryptInstructions(), "age -d") {
		t.Error("VaultDecryptInstructions must not mention `age -d`")
	}
	// The corrected README points at the wizard.
	if !strings.Contains(VaultReadme(), "kawarimi open") {
		t.Error("VaultReadme should point recipients at `kawarimi open`")
	}
}
