package vault

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/olemoudi/kawarimi/internal/crypto"
)

const (
	ManifestFile           = "manifest.age"
	ReadmeFile             = "README.md"
	DecryptInstructionsFile = "DECRYPT_INSTRUCTIONS.md"
	LastCheckinFile        = "last_checkin"
)

// Vault represents an open vault on disk.
type Vault struct {
	Dir        string
	Passphrase string
	Manifest   *Manifest
}

// Create initializes a new vault at the given directory.
func Create(dir string, passphrase string) (*Vault, error) {
	if _, err := os.Stat(dir); err == nil {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if e.Name() == ManifestFile {
				return nil, fmt.Errorf("vault already exists at %s", dir)
			}
		}
	}

	dirs := []string{
		dir,
		filepath.Join(dir, string(CategoryNotes)),
		filepath.Join(dir, string(CategoryCredentials)),
		filepath.Join(dir, string(CategoryDocuments)),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0700); err != nil {
			return nil, fmt.Errorf("creating directory %s: %w", d, err)
		}
	}

	if err := os.WriteFile(filepath.Join(dir, ReadmeFile), []byte(readmeContent), 0644); err != nil {
		return nil, fmt.Errorf("writing README: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, DecryptInstructionsFile), []byte(decryptInstructionsContent), 0644); err != nil {
		return nil, fmt.Errorf("writing decrypt instructions: %w", err)
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

// Open loads an existing vault from the given directory.
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

// SaveManifestToDisk writes the current manifest to the vault directory.
func (v *Vault) SaveManifestToDisk() error {
	return SaveManifest(filepath.Join(v.Dir, ManifestFile), v.Manifest, v.Passphrase)
}

// AddNote encrypts a markdown note and adds it to the vault.
func (v *Vault) AddNote(title string, content []byte, tags []string) (*Entry, error) {
	name := Slugify(title)
	seq := v.Manifest.NextSeq(CategoryNotes)
	filename := BuildFilename(CategoryNotes, seq, name, ".md")

	filePath := filepath.Join(v.Dir, filename)
	if err := crypto.EncryptFile(filePath, content, v.Passphrase); err != nil {
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
	if err := crypto.EncryptFile(filePath, data, v.Passphrase); err != nil {
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
	ext := filepath.Ext(originalName)
	seq := v.Manifest.NextSeq(CategoryDocuments)
	filename := BuildFilename(CategoryDocuments, seq, name, ext)

	filePath := filepath.Join(v.Dir, filename)
	if err := crypto.EncryptFile(filePath, data, v.Passphrase); err != nil {
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
	return crypto.DecryptFile(filePath, v.Passphrase)
}

// UpdateEntry re-encrypts an entry with new content and updates the manifest timestamp.
func (v *Vault) UpdateEntry(entry *Entry, newContent []byte) error {
	filePath := filepath.Join(v.Dir, entry.Filename)
	if err := crypto.EncryptFile(filePath, newContent, v.Passphrase); err != nil {
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
		if _, err := crypto.DecryptFile(filePath, v.Passphrase); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", entry.Filename, err))
		}
	}
	return errs
}

// Export decrypts the entire vault to the given output directory.
func (v *Vault) Export(outputDir string) error {
	dirs := []string{
		filepath.Join(outputDir, string(CategoryNotes)),
		filepath.Join(outputDir, string(CategoryCredentials)),
		filepath.Join(outputDir, string(CategoryDocuments)),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0700); err != nil {
			return fmt.Errorf("creating output directory %s: %w", d, err)
		}
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
