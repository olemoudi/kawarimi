package copytext

import (
	"regexp"
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

// The numbered STEPS must not hardcode binary names (the shipped set varies);
// they point at the per-OS legend instead and disambiguate the two Mac builds.
func TestPackageInstructionsStepsReferenceTheLegend(t *testing.T) {
	got := PackageInstructions([]string{"kawarimi-darwin-arm64", "kawarimi-darwin-amd64"}, "2026-07-05")

	if strings.Contains(got, "linux-amd64") {
		t.Error("darwin-only package must not mention linux binaries anywhere")
	}
	for _, want := range []string{
		`("Programas incluidos")`, `("Programs included")`, // steps point at the legend
		"Apple Silicon", "Intel", // Mac model guidance
		"termina en .exe", "ending in .exe", // Windows without a hardcoded name
	} {
		if !strings.Contains(got, want) {
			t.Errorf("instructions missing %q", want)
		}
	}
}

// The workflow variant is embedded in a bash heredoc inside YAML: it must stay
// pure ASCII (MIME-conservative for the raw curl upload) and must not contain
// anything bash would expand or that would terminate the heredoc early.
func TestReleaseEmailWorkflowBodyHeredocSafe(t *testing.T) {
	body := ReleaseEmailBodyWorkflow()

	for i, r := range body {
		if r > 127 {
			t.Fatalf("non-ASCII rune %q at byte %d — the heredoc must stay ASCII", r, i)
		}
	}
	for _, forbidden := range []string{"`", "$("} {
		if strings.Contains(body, forbidden) {
			t.Errorf("heredoc body must not contain %q (bash would expand it)", forbidden)
		}
	}
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "EOF" {
			t.Error("a bare EOF line would terminate the heredoc early")
		}
	}
	// Only the three known placeholders may appear as $NAMES.
	for _, m := range regexp.MustCompile(`\$[A-Za-z_]+`).FindAllString(body, -1) {
		switch m {
		case "$DMS_KEY", "$VAULT_PACKAGE_LOCATION", "$DAYS":
		default:
			t.Errorf("unexpected shell variable %q in the release email body", m)
		}
	}
	for _, want := range []string{"ESPANOL", "ENGLISH", "INDEX.md", "TARJETA", "CARD"} {
		if !strings.Contains(body, want) {
			t.Errorf("workflow release email missing %q", want)
		}
	}
}

// Both release-email variants come from one builder; this pins that they carry the
// same step structure so they can never drift apart again.
func TestReleaseEmailBodiesDoNotDrift(t *testing.T) {
	local := ReleaseEmailBody("https://example.test/vault.zip", "KEY123")
	workflow := ReleaseEmailBodyWorkflow()

	markers := []string{
		"IMPORTANT", "TARJETA", "CARD", // the card explained before the steps
		"INSTRUCTIONS.md", "decrypted", "INDEX.md",
		"paste this text:", "pega este texto:", // load-bearing: the story test reads the key after this phrase
	}
	for _, m := range markers {
		inLocal := strings.Contains(local, m)
		inWorkflow := strings.Contains(workflow, m)
		if !inLocal || !inWorkflow {
			t.Errorf("marker %q: local=%v workflow=%v — the two bodies drifted", m, inLocal, inWorkflow)
		}
	}
	// The card must be explained BEFORE the numbered steps in both.
	for name, body := range map[string]string{"local": local, "workflow": workflow} {
		if strings.Index(body, "CARD") > strings.Index(body, "1. Download") {
			t.Errorf("%s: the card explanation must come before the steps", name)
		}
	}
}
