package vault

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/olemoudi/kawarimi/internal/atomicfile"
	"github.com/olemoudi/kawarimi/internal/copytext"
	"github.com/olemoudi/kawarimi/internal/crypto"
)

const (
	ManifestFile            = "manifest.age"
	ReadmeFile              = "README.md"
	DecryptInstructionsFile = "DECRYPT_INSTRUCTIONS.md"
	LastCheckinFile         = "last_checkin"
)

// Vault represents an open vault on disk.
type Vault struct {
	Dir          string
	AgeIdentity  string // V2: X25519 identity for decryption
	AgeRecipient string // V2: X25519 recipient for encryption
	Manifest     *Manifest
	// Deprecated: only used for v1 vault compatibility and migration
	Passphrase string
}

// isV2 returns true if this vault uses the new identity-based encryption.
func (v *Vault) isV2() bool {
	return v.AgeIdentity != "" && v.AgeRecipient != ""
}

// Create initializes a new v1 vault at the given directory.
// Deprecated: use CreateV2 for new vaults.
func Create(dir string, passphrase string) (*Vault, error) {
	if _, err := os.Stat(dir); err == nil {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if e.Name() == ManifestFile {
				return nil, fmt.Errorf("vault already exists at %s", dir)
			}
		}
	}

	if err := createVaultDirs(dir); err != nil {
		return nil, err
	}

	if err := writeVaultReadme(dir); err != nil {
		return nil, err
	}

	manifest := NewManifest()
	manifestPath := filepath.Join(dir, ManifestFile)
	if err := SaveManifest(manifestPath, manifest, passphrase); err != nil {
		return nil, fmt.Errorf("saving initial manifest: %w", err)
	}

	return &Vault{
		Dir:        dir,
		Passphrase: passphrase,
		Manifest:   manifest,
	}, nil
}

// Open loads an existing v1 vault from the given directory.
// Deprecated: use OpenV2 for v2 vaults.
func Open(dir string, passphrase string) (*Vault, error) {
	manifestPath := filepath.Join(dir, ManifestFile)
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("no vault found at %s (missing %s)", dir, ManifestFile)
	}

	manifest, err := LoadManifest(manifestPath, passphrase)
	if err != nil {
		return nil, fmt.Errorf("opening vault: %w", err)
	}

	return &Vault{
		Dir:        dir,
		Passphrase: passphrase,
		Manifest:   manifest,
	}, nil
}

// CreateV2 initializes a new v2 vault with identity-based encryption.
// The vault header should already be written to disk before calling this.
func CreateV2(dir string, ageIdentity, ageRecipient string) (*Vault, error) {
	if _, err := os.Stat(dir); err == nil {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if e.Name() == ManifestFile {
				return nil, fmt.Errorf("vault already exists at %s", dir)
			}
		}
	}

	if err := createVaultDirs(dir); err != nil {
		return nil, err
	}

	if err := writeVaultReadme(dir); err != nil {
		return nil, err
	}

	manifest := NewManifest()
	manifestPath := filepath.Join(dir, ManifestFile)
	if err := SaveManifestV2(manifestPath, manifest, ageRecipient); err != nil {
		return nil, fmt.Errorf("saving initial manifest: %w", err)
	}

	return &Vault{
		Dir:          dir,
		AgeIdentity:  ageIdentity,
		AgeRecipient: ageRecipient,
		Manifest:     manifest,
	}, nil
}

// OpenV2 loads an existing v2 vault using an age identity.
func OpenV2(dir string, ageIdentity, ageRecipient string) (*Vault, error) {
	manifestPath := filepath.Join(dir, ManifestFile)
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("no vault found at %s (missing %s)", dir, ManifestFile)
	}

	manifest, err := LoadManifestV2(manifestPath, ageIdentity)
	if err != nil {
		return nil, fmt.Errorf("opening vault: %w", err)
	}

	return &Vault{
		Dir:          dir,
		AgeIdentity:  ageIdentity,
		AgeRecipient: ageRecipient,
		Manifest:     manifest,
	}, nil
}

// SaveManifestToDisk writes the current manifest to the vault directory.
func (v *Vault) SaveManifestToDisk() error {
	path := filepath.Join(v.Dir, ManifestFile)
	if v.isV2() {
		return SaveManifestV2(path, v.Manifest, v.AgeRecipient)
	}
	return SaveManifest(path, v.Manifest, v.Passphrase)
}

// encryptData encrypts data using the vault's encryption method.
func (v *Vault) encryptData(plaintext []byte) ([]byte, error) {
	if v.isV2() {
		return EncryptWithIdentity(plaintext, v.AgeRecipient)
	}
	return crypto.Encrypt(plaintext, v.Passphrase)
}

// decryptData decrypts data using the vault's decryption method.
func (v *Vault) decryptData(ciphertext []byte) ([]byte, error) {
	if v.isV2() {
		return DecryptWithIdentity(ciphertext, v.AgeIdentity)
	}
	return crypto.Decrypt(ciphertext, v.Passphrase)
}

// encryptFile encrypts data and writes it to disk atomically.
func (v *Vault) encryptFile(path string, plaintext []byte) error {
	ciphertext, err := v.encryptData(plaintext)
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(path, ciphertext, 0600)
}

// decryptFile reads and decrypts a file.
func (v *Vault) decryptFile(path string) ([]byte, error) {
	ciphertext, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading file %s: %w", path, err)
	}
	return v.decryptData(ciphertext)
}

// AddNote encrypts a markdown note and adds it to the vault.
func (v *Vault) AddNote(title string, content []byte, tags []string) (*Entry, error) {
	name := Slugify(title)
	seq := v.Manifest.NextSeq(CategoryNotes)
	filename := BuildFilename(CategoryNotes, seq, name, ".md")

	filePath := filepath.Join(v.Dir, filename)
	if err := v.encryptFile(filePath, content); err != nil {
		return nil, fmt.Errorf("encrypting note: %w", err)
	}

	now := NowUTC()
	entry := &Entry{
		ID:          GenerateID(),
		Category:    CategoryNotes,
		Name:        name,
		Title:       title,
		Filename:    filename,
		ContentType: "text/markdown",
		CreatedAt:   now,
		UpdatedAt:   now,
		Tags:        tags,
	}

	v.Manifest.AddEntry(entry)
	if err := v.SaveManifestToDisk(); err != nil {
		return nil, fmt.Errorf("saving manifest after adding note: %w", err)
	}

	return entry, nil
}

// AddCredential encrypts a credential and adds it to the vault.
func (v *Vault) AddCredential(cred *Credential, tags []string) (*Entry, error) {
	name := Slugify(cred.Service)
	seq := v.Manifest.NextSeq(CategoryCredentials)
	filename := BuildFilename(CategoryCredentials, seq, name, ".json")

	data, err := marshalJSON(cred)
	if err != nil {
		return nil, fmt.Errorf("marshaling credential: %w", err)
	}

	filePath := filepath.Join(v.Dir, filename)
	if err := v.encryptFile(filePath, data); err != nil {
		return nil, fmt.Errorf("encrypting credential: %w", err)
	}

	title := cred.Service
	if cred.Username != "" {
		title += " - " + cred.Username
	}

	now := NowUTC()
	entry := &Entry{
		ID:          GenerateID(),
		Category:    CategoryCredentials,
		Name:        name,
		Title:       title,
		Filename:    filename,
		ContentType: "application/json",
		CreatedAt:   now,
		UpdatedAt:   now,
		Tags:        tags,
	}

	v.Manifest.AddEntry(entry)
	if err := v.SaveManifestToDisk(); err != nil {
		return nil, fmt.Errorf("saving manifest after adding credential: %w", err)
	}

	return entry, nil
}

// AddDocument encrypts a binary file and adds it to the vault.
func (v *Vault) AddDocument(title string, originalName string, data []byte, tags []string) (*Entry, error) {
	name := Slugify(title)
	ext := filepath.Ext(filepath.Base(originalName)) // Base() strips directory components
	seq := v.Manifest.NextSeq(CategoryDocuments)
	filename := BuildFilename(CategoryDocuments, seq, name, ext)

	filePath := filepath.Join(v.Dir, filename)
	// Validate the resolved path stays within the vault directory
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, fmt.Errorf("resolving path: %w", err)
	}
	absVault, _ := filepath.Abs(v.Dir)
	if !strings.HasPrefix(absPath, absVault+string(filepath.Separator)) {
		return nil, fmt.Errorf("path traversal detected: %s escapes vault", filename)
	}

	if err := v.encryptFile(filePath, data); err != nil {
		return nil, fmt.Errorf("encrypting document: %w", err)
	}

	now := NowUTC()
	entry := &Entry{
		ID:          GenerateID(),
		Category:    CategoryDocuments,
		Name:        name,
		Title:       title,
		Filename:    filename,
		ContentType: contentTypeFromExt(ext),
		CreatedAt:   now,
		UpdatedAt:   now,
		Tags:        tags,
	}

	v.Manifest.AddEntry(entry)
	if err := v.SaveManifestToDisk(); err != nil {
		return nil, fmt.Errorf("saving manifest after adding document: %w", err)
	}

	return entry, nil
}

// ShowEntry decrypts and returns the content of a vault entry.
func (v *Vault) ShowEntry(entry *Entry) ([]byte, error) {
	filePath := filepath.Join(v.Dir, entry.Filename)
	return v.decryptFile(filePath)
}

// UpdateEntry re-encrypts an entry with new content and updates the manifest timestamp.
func (v *Vault) UpdateEntry(entry *Entry, newContent []byte) error {
	filePath := filepath.Join(v.Dir, entry.Filename)
	if err := v.encryptFile(filePath, newContent); err != nil {
		return fmt.Errorf("re-encrypting entry: %w", err)
	}
	entry.UpdatedAt = NowUTC()
	v.Manifest.UpdatedAt = NowUTC()
	return v.SaveManifestToDisk()
}

// RemoveEntry removes an entry from the manifest and deletes the encrypted file.
func (v *Vault) RemoveEntry(id string) (*Entry, error) {
	entry := v.Manifest.RemoveEntry(id)
	if entry == nil {
		return nil, fmt.Errorf("entry not found: %s", id)
	}

	filePath := filepath.Join(v.Dir, entry.Filename)
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return entry, fmt.Errorf("deleting file %s: %w", entry.Filename, err)
	}

	if err := v.SaveManifestToDisk(); err != nil {
		return entry, fmt.Errorf("saving manifest after removal: %w", err)
	}

	return entry, nil
}

// Verify checks that all entries in the manifest can be decrypted.
func (v *Vault) Verify() []error {
	var errs []error
	for _, entry := range v.Manifest.Entries {
		filePath := filepath.Join(v.Dir, entry.Filename)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("%s: file missing", entry.Filename))
			continue
		}
		if _, err := v.decryptFile(filePath); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", entry.Filename, err))
		}
	}
	return errs
}

// Export decrypts the entire vault to the given output directory. Category
// directories are created only for entries that exist — an empty folder is just
// noise for the recipient browsing the output.
func (v *Vault) Export(outputDir string) error {
	if err := os.MkdirAll(outputDir, 0700); err != nil {
		return fmt.Errorf("creating output directory %s: %w", outputDir, err)
	}

	absOutputDir, err := filepath.Abs(outputDir)
	if err != nil {
		return fmt.Errorf("resolving output directory: %w", err)
	}

	for _, entry := range v.Manifest.Entries {
		data, err := v.ShowEntry(entry)
		if err != nil {
			return fmt.Errorf("decrypting %s: %w", entry.Filename, err)
		}

		// Strip the .age extension for the output filename
		outName := entry.Filename
		if len(outName) > 4 && outName[len(outName)-4:] == ".age" {
			outName = outName[:len(outName)-4]
		}
		outPath := filepath.Join(outputDir, outName)

		// Validate path stays within output directory
		absOutPath, err := filepath.Abs(outPath)
		if err != nil {
			return fmt.Errorf("resolving path for %s: %w", outName, err)
		}
		if !strings.HasPrefix(absOutPath, absOutputDir+string(filepath.Separator)) {
			return fmt.Errorf("path traversal detected in entry filename: %s", entry.Filename)
		}

		if err := os.MkdirAll(filepath.Dir(outPath), 0700); err != nil {
			return fmt.Errorf("creating output directory for %s: %w", outName, err)
		}
		if err := os.WriteFile(outPath, data, 0600); err != nil {
			return fmt.Errorf("writing %s: %w", outPath, err)
		}
	}

	index := generateIndex(v.Manifest)
	indexPath := filepath.Join(outputDir, "INDEX.md")
	if err := os.WriteFile(indexPath, []byte(index), 0644); err != nil {
		return fmt.Errorf("writing INDEX.md: %w", err)
	}

	return nil
}

// --- Internal helpers ---

func createVaultDirs(dir string) error {
	dirs := []string{
		dir,
		filepath.Join(dir, string(CategoryNotes)),
		filepath.Join(dir, string(CategoryCredentials)),
		filepath.Join(dir, string(CategoryDocuments)),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0700); err != nil {
			return fmt.Errorf("creating directory %s: %w", d, err)
		}
	}
	return nil
}

func writeVaultReadme(dir string) error {
	if err := os.WriteFile(filepath.Join(dir, ReadmeFile), []byte(copytext.VaultReadme()), 0644); err != nil {
		return fmt.Errorf("writing README: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, DecryptInstructionsFile), []byte(copytext.VaultDecryptInstructions()), 0644); err != nil {
		return fmt.Errorf("writing decrypt instructions: %w", err)
	}
	return nil
}
