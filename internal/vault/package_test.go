package vault_test

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olemoudi/kawarimi/internal/crypto"
	"github.com/olemoudi/kawarimi/internal/vault"
)

func readZipEntry(t *testing.T, r *zip.ReadCloser, name string) string {
	t.Helper()
	for _, f := range r.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("opening %s in zip: %v", name, err)
			}
			defer rc.Close()
			data, err := io.ReadAll(rc)
			if err != nil {
				t.Fatalf("reading %s in zip: %v", name, err)
			}
			return string(data)
		}
	}
	t.Fatalf("%s not found in zip", name)
	return ""
}

func createTestVaultWithDir(t *testing.T) (string, *vault.Vault) {
	t.Helper()
	dir := t.TempDir()

	tp := crypto.TestParams()
	params := vault.InitParams{
		Password:          "test-password",
		DeviceID:          "test-device",
		MnemonicKDFParams: &tp,
		OwnerKDFParams:    &tp,
	}
	result, err := vault.NewHeader(params)
	if err != nil {
		t.Fatalf("NewHeader: %v", err)
	}
	defer crypto.ZeroBytes(result.MasterKey)

	if err := vault.SaveHeader(dir, result.Header); err != nil {
		t.Fatalf("SaveHeader: %v", err)
	}

	v, err := vault.CreateV2(dir, result.AgeIdentity, result.Header.AgeRecipient)
	if err != nil {
		t.Fatalf("CreateV2: %v", err)
	}

	// Add a test note
	_, err = v.AddNote("Test Note", []byte("# Test\nSome content"), nil)
	if err != nil {
		t.Fatalf("AddNote: %v", err)
	}

	return dir, v
}

func TestBuildPackage(t *testing.T) {
	vaultDir, _ := createTestVaultWithDir(t)
	outputDir := t.TempDir()
	outputPath := filepath.Join(outputDir, "test-vault.zip")

	err := vault.BuildPackage(vaultDir, outputPath, "")
	if err != nil {
		t.Fatalf("BuildPackage: %v", err)
	}

	// Verify zip exists
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("stat output: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("output zip is empty")
	}

	// Read zip contents
	r, err := zip.OpenReader(outputPath)
	if err != nil {
		t.Fatalf("opening zip: %v", err)
	}
	defer r.Close()

	fileNames := make(map[string]bool)
	for _, f := range r.File {
		fileNames[f.Name] = true
	}

	// Check required files
	required := []string{
		"vault/vault_header.json",
		"vault/manifest.age",
		"vault/README.md",
		"INSTRUCTIONS.md",
	}
	for _, name := range required {
		if !fileNames[name] {
			t.Errorf("missing required file in package: %s", name)
		}
	}

	// Verify no secrets in zip (no device.key, no switch files)
	for name := range fileNames {
		if strings.Contains(name, "device.key") ||
			strings.Contains(name, "switch-") ||
			strings.Contains(name, "last_checkin") {
			t.Errorf("package contains sensitive file: %s", name)
		}
	}
}

func TestBuildPackageWithBinaries(t *testing.T) {
	vaultDir, _ := createTestVaultWithDir(t)
	outputDir := t.TempDir()
	outputPath := filepath.Join(outputDir, "test-vault.zip")

	// Create fake binaries
	binDir := filepath.Join(outputDir, "binaries")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("creating bin dir: %v", err)
	}
	for _, name := range []string{"kawarimi-linux-amd64", "kawarimi-darwin-arm64", "kawarimi-windows-amd64.exe"} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte("fake-binary"), 0755); err != nil {
			t.Fatalf("writing fake binary: %v", err)
		}
	}
	// Non-kawarimi file should be skipped
	if err := os.WriteFile(filepath.Join(binDir, "other-tool"), []byte("not included"), 0755); err != nil {
		t.Fatalf("writing other tool: %v", err)
	}

	err := vault.BuildPackage(vaultDir, outputPath, binDir)
	if err != nil {
		t.Fatalf("BuildPackage: %v", err)
	}

	r, err := zip.OpenReader(outputPath)
	if err != nil {
		t.Fatalf("opening zip: %v", err)
	}
	defer r.Close()

	binaries := 0
	hasOther := false
	for _, f := range r.File {
		if strings.HasPrefix(f.Name, "kawarimi-") {
			binaries++
			// Check executable permissions
			if f.Mode().Perm()&0111 == 0 {
				t.Errorf("binary %s should be executable", f.Name)
			}
		}
		if f.Name == "other-tool" {
			hasOther = true
		}
	}

	if binaries != 3 {
		t.Errorf("expected 3 binaries, got %d", binaries)
	}
	if hasOther {
		t.Error("non-kawarimi file should not be included")
	}

	// INSTRUCTIONS.md must be bilingual, list the bundled binaries, and never
	// mention the age CLI (which cannot open a V2/V4 vault).
	instr := readZipEntry(t, r, "INSTRUCTIONS.md")
	for _, want := range []string{"ESPAÑOL", "ENGLISH", "kawarimi-linux-amd64", "kawarimi-windows-amd64.exe", "INDEX.md"} {
		if !strings.Contains(instr, want) {
			t.Errorf("INSTRUCTIONS.md missing %q", want)
		}
	}
	if strings.Contains(instr, "age -d") {
		t.Error("INSTRUCTIONS.md must not mention `age -d`")
	}
}

func TestExtractPackage(t *testing.T) {
	vaultDir, _ := createTestVaultWithDir(t)
	outputDir := t.TempDir()
	packagePath := filepath.Join(outputDir, "test-vault.zip")

	err := vault.BuildPackage(vaultDir, packagePath, "")
	if err != nil {
		t.Fatalf("BuildPackage: %v", err)
	}

	// Extract to a new directory
	extractDir := filepath.Join(outputDir, "extracted")
	vaultPath, err := vault.ExtractPackage(packagePath, extractDir)
	if err != nil {
		t.Fatalf("ExtractPackage: %v", err)
	}

	// Verify vault path
	expectedVaultPath := filepath.Join(extractDir, "vault")
	if vaultPath != expectedVaultPath {
		t.Errorf("vault path = %q, want %q", vaultPath, expectedVaultPath)
	}

	// Verify vault header exists in extracted location
	headerPath := filepath.Join(vaultPath, "vault_header.json")
	if _, err := os.Stat(headerPath); os.IsNotExist(err) {
		t.Fatal("extracted vault header not found")
	}

	// Verify instructions
	instrPath := filepath.Join(extractDir, "INSTRUCTIONS.md")
	if _, err := os.Stat(instrPath); os.IsNotExist(err) {
		t.Fatal("INSTRUCTIONS.md not found in extracted package")
	}
}

func TestBuildPackageNoVault(t *testing.T) {
	outputPath := filepath.Join(t.TempDir(), "test.zip")
	err := vault.BuildPackage("/nonexistent/vault", outputPath, "")
	if err == nil {
		t.Fatal("expected error for missing vault")
	}
}
