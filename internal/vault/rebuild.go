package vault

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// RebuildManifest reconstructs manifest.age by walking the encrypted entry files and
// decrypting each with the vault identity. It is the recovery path for a lost or
// corrupt manifest: metadata that lived only in the manifest (tags, exact titles,
// timestamps) is approximated from the filename and file mtime, but every
// decryptable entry is re-indexed so the vault — and Export for the recipient —
// work again. Files that do not decrypt with this identity are skipped, not indexed.
// Returns the rebuilt manifest and how many entries it recovered.
func RebuildManifest(vaultDir, ageIdentity, ageRecipient string) (*Manifest, int, error) {
	m := NewManifest()
	for _, cat := range []Category{CategoryNotes, CategoryCredentials, CategoryDocuments} {
		dir := filepath.Join(vaultDir, string(cat))
		files, err := os.ReadDir(dir)
		if err != nil {
			continue // a category dir may legitimately not exist
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".age") {
				continue
			}
			rel := filepath.Join(string(cat), f.Name())
			ciphertext, err := os.ReadFile(filepath.Join(vaultDir, rel))
			if err != nil {
				return nil, 0, fmt.Errorf("reading %s: %w", rel, err)
			}
			if _, err := DecryptWithIdentity(ciphertext, ageIdentity); err != nil {
				continue // not ours / unreadable — don't index a file we can't open
			}
			ts := NowUTC()
			if info, err := f.Info(); err == nil {
				ts = info.ModTime().UTC().Format(time.RFC3339)
			}
			name := reconstructEntryName(f.Name())
			m.Entries = append(m.Entries, &Entry{
				ID:          GenerateID(),
				Category:    cat,
				Name:        name,
				Title:       name,
				Filename:    rel,
				ContentType: contentTypeForCategory(cat, f.Name()),
				CreatedAt:   ts,
				UpdatedAt:   ts,
			})
		}
	}
	if err := SaveManifestV2(filepath.Join(vaultDir, ManifestFile), m, ageRecipient); err != nil {
		return nil, 0, fmt.Errorf("saving rebuilt manifest: %w", err)
	}
	return m, len(m.Entries), nil
}

// reconstructEntryName recovers a human-ish name from an entry filename like
// "001-bank-accounts.md.age" → "bank-accounts".
func reconstructEntryName(base string) string {
	n := strings.TrimSuffix(base, ".age")
	n = strings.TrimSuffix(n, filepath.Ext(n)) // drop .md / .json / .pdf ...
	if i := strings.IndexByte(n, '-'); i > 0 {
		if _, err := strconv.Atoi(n[:i]); err == nil {
			n = n[i+1:] // drop the NNN- sequence prefix
		}
	}
	if n == "" {
		return base
	}
	return n
}

func contentTypeForCategory(cat Category, filename string) string {
	switch cat {
	case CategoryNotes:
		return "text/markdown"
	case CategoryCredentials:
		return "application/json"
	default:
		return contentTypeFromExt(filepath.Ext(strings.TrimSuffix(filename, ".age")))
	}
}
