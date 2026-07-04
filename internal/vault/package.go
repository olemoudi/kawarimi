package vault

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/olemoudi/kawarimi/internal/copytext"
)

const (
	// PackageVaultDir is the directory name inside the zip for vault files.
	PackageVaultDir = "vault"
	// PackageInstructionsFile is the instructions file inside the zip.
	PackageInstructionsFile = "INSTRUCTIONS.md"
	// SealedPayloadFile is the sealed payload file stored in the vault directory (V4).
	SealedPayloadFile = "sealed_payload.age"
)

// BuildPackage creates a distributable vault package (zip) containing:
// - vault/ directory with all encrypted vault files
// - kawarimi binaries for each platform (from binariesDir)
// - INSTRUCTIONS.md for recipients
//
// No secrets are included in the package. The vault files are encrypted
// and can only be opened with the mnemonic (which is sealed separately).
func BuildPackage(vaultDir string, outputPath string, binariesDir string) error {
	// Validate vault exists
	headerPath := filepath.Join(vaultDir, HeaderFile)
	if _, err := os.Stat(headerPath); os.IsNotExist(err) {
		return fmt.Errorf("no vault header found at %s", vaultDir)
	}

	out, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("creating package file: %w", err)
	}
	defer out.Close()

	w := zip.NewWriter(out)
	defer w.Close()

	// Add vault files (the on-disk recipient docs are skipped by addDirToZip)
	if err := addDirToZip(w, vaultDir, PackageVaultDir); err != nil {
		return fmt.Errorf("adding vault to package: %w", err)
	}

	// Inject fresh, correct recipient docs into the packaged vault dir.
	injected := map[string]string{
		ReadmeFile:              copytext.VaultReadme(),
		DecryptInstructionsFile: copytext.VaultDecryptInstructions(),
	}
	for name, content := range injected {
		zipPath := PackageVaultDir + "/" + name
		fw, err := w.Create(zipPath)
		if err != nil {
			return fmt.Errorf("creating %s in package: %w", zipPath, err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			return fmt.Errorf("writing %s to package: %w", zipPath, err)
		}
	}

	// Add binaries if provided
	var binaries []string
	if binariesDir != "" {
		names, err := addBinariesToZip(w, binariesDir)
		if err != nil {
			return fmt.Errorf("adding binaries to package: %w", err)
		}
		binaries = names
	}

	// Add instructions (bilingual; lists exactly the binaries that shipped)
	instructions := copytext.PackageInstructions(binaries, time.Now().UTC().Format("2006-01-02"))
	iw, err := w.Create(PackageInstructionsFile)
	if err != nil {
		return fmt.Errorf("creating instructions entry: %w", err)
	}
	if _, err := iw.Write([]byte(instructions)); err != nil {
		return fmt.Errorf("writing instructions: %w", err)
	}

	return nil
}

// ExtractPackage extracts a vault package zip to the given directory.
// Returns the path to the vault directory within the extracted contents.
func ExtractPackage(packagePath string, destDir string) (string, error) {
	r, err := zip.OpenReader(packagePath)
	if err != nil {
		return "", fmt.Errorf("opening package: %w", err)
	}
	defer r.Close()

	absDestDir, err := filepath.Abs(destDir)
	if err != nil {
		return "", fmt.Errorf("resolving destination: %w", err)
	}

	var total int64
	for _, f := range r.File {
		targetPath := filepath.Join(destDir, f.Name)

		// Path traversal protection
		absPath, err := filepath.Abs(targetPath)
		if err != nil {
			return "", fmt.Errorf("resolving path: %w", err)
		}
		if !strings.HasPrefix(absPath, absDestDir+string(filepath.Separator)) && absPath != absDestDir {
			return "", fmt.Errorf("path traversal detected: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, 0700); err != nil {
				return "", fmt.Errorf("creating directory: %w", err)
			}
			continue
		}

		// Ensure parent directory exists
		if err := os.MkdirAll(filepath.Dir(targetPath), 0700); err != nil {
			return "", fmt.Errorf("creating parent directory: %w", err)
		}

		written, err := extractFile(f, targetPath)
		if err != nil {
			return "", err
		}
		total += written
		if total > maxPackageTotalBytes {
			return "", fmt.Errorf("package expands to more than %d bytes — refusing (possible zip bomb)", maxPackageTotalBytes)
		}
	}

	return filepath.Join(destDir, PackageVaultDir), nil
}

// Decompressed-size caps guard against zip bombs while staying generous enough for
// real vaults with document attachments and the bundled per-platform binaries.
const (
	maxPackageEntryBytes = 500 << 20 // 500 MiB per entry
	maxPackageTotalBytes = 2 << 30   // 2 GiB total
)

func extractFile(f *zip.File, targetPath string) (int64, error) {
	rc, err := f.Open()
	if err != nil {
		return 0, fmt.Errorf("opening %s in zip: %w", f.Name, err)
	}
	defer rc.Close()

	// Preserve executable permissions for binaries
	perm := f.Mode().Perm()
	if perm == 0 {
		perm = 0600
	}

	out, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return 0, fmt.Errorf("creating %s: %w", targetPath, err)
	}
	defer out.Close()

	// Cap the decompressed size per entry (a lying zip header can't exceed this).
	n, err := io.Copy(out, io.LimitReader(rc, maxPackageEntryBytes+1))
	if err != nil {
		return n, fmt.Errorf("extracting %s: %w", f.Name, err)
	}
	if n > maxPackageEntryBytes {
		return n, fmt.Errorf("entry %s exceeds the %d-byte limit — refusing (possible zip bomb)", f.Name, maxPackageEntryBytes)
	}
	return n, nil
}

func addDirToZip(w *zip.Writer, srcDir string, zipPrefix string) error {
	absSrc, err := filepath.Abs(srcDir)
	if err != nil {
		return err
	}

	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip .git directory
		if info.IsDir() && info.Name() == ".git" {
			return filepath.SkipDir
		}

		// Skip last_checkin file (DMS state, not part of vault content)
		if info.Name() == LastCheckinFile {
			return nil
		}

		// Skip the on-disk recipient docs; BuildPackage injects fresh copies so
		// even vaults created before the bilingual rewrite ship correct
		// instructions instead of the old age-CLI ones.
		if !info.IsDir() && (info.Name() == ReadmeFile || info.Name() == DecryptInstructionsFile) {
			return nil
		}

		absPath, err := filepath.Abs(path)
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(absSrc, absPath)
		if err != nil {
			return err
		}

		zipPath := filepath.Join(zipPrefix, relPath)
		// Normalize to forward slashes for zip
		zipPath = filepath.ToSlash(zipPath)

		if info.IsDir() {
			_, err := w.Create(zipPath + "/")
			return err
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}

		fw, err := w.Create(zipPath)
		if err != nil {
			return fmt.Errorf("creating zip entry %s: %w", zipPath, err)
		}

		if _, err := fw.Write(data); err != nil {
			return fmt.Errorf("writing %s to zip: %w", zipPath, err)
		}

		return nil
	})
}

// addBinariesToZip copies every kawarimi-* file from binariesDir into the zip and
// returns the names it added, so the instructions can list exactly what shipped.
func addBinariesToZip(w *zip.Writer, binariesDir string) ([]string, error) {
	entries, err := os.ReadDir(binariesDir)
	if err != nil {
		return nil, fmt.Errorf("reading binaries directory: %w", err)
	}

	var added []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		// Only include kawarimi binaries
		if !strings.HasPrefix(name, "kawarimi") {
			continue
		}

		path := filepath.Join(binariesDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading binary %s: %w", name, err)
		}

		// Create with executable permissions
		header := &zip.FileHeader{
			Name:   name,
			Method: zip.Deflate,
		}
		header.SetMode(0755)

		fw, err := w.CreateHeader(header)
		if err != nil {
			return nil, fmt.Errorf("creating zip entry for %s: %w", name, err)
		}

		if _, err := fw.Write(data); err != nil {
			return nil, fmt.Errorf("writing %s to zip: %w", name, err)
		}
		added = append(added, name)
	}

	return added, nil
}
