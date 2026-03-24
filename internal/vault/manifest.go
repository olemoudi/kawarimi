package vault

import (
	"encoding/json"
	"fmt"

	"github.com/olemoudi/kawarimi/internal/crypto"
)

// Manifest holds the vault index — the list of all entries and metadata.
type Manifest struct {
	Version   int      `json:"version"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
	Entries   []*Entry `json:"entries"`
}

// NewManifest creates a new empty manifest.
func NewManifest() *Manifest {
	now := NowUTC()
	return &Manifest{
		Version:   1,
		CreatedAt: now,
		UpdatedAt: now,
		Entries:   []*Entry{},
	}
}

// AddEntry appends an entry to the manifest and updates the timestamp.
func (m *Manifest) AddEntry(e *Entry) {
	m.Entries = append(m.Entries, e)
	m.UpdatedAt = NowUTC()
}

// RemoveEntry removes an entry by ID. Returns the removed entry or nil.
func (m *Manifest) RemoveEntry(id string) *Entry {
	for i, e := range m.Entries {
		if e.ID == id {
			m.Entries = append(m.Entries[:i], m.Entries[i+1:]...)
			m.UpdatedAt = NowUTC()
			return e
		}
	}
	return nil
}

// FindEntry finds an entry by ID or name (case-insensitive).
func (m *Manifest) FindEntry(query string) *Entry {
	for _, e := range m.Entries {
		if e.ID == query || e.Name == query {
			return e
		}
	}
	return nil
}

// FindEntriesByCategory returns all entries matching the given category.
func (m *Manifest) FindEntriesByCategory(cat Category) []*Entry {
	var result []*Entry
	for _, e := range m.Entries {
		if e.Category == cat {
			result = append(result, e)
		}
	}
	return result
}

// NextSeq returns the next sequence number for a given category.
func (m *Manifest) NextSeq(cat Category) int {
	max := 0
	for _, e := range m.Entries {
		if e.Category == cat {
			max++
		}
	}
	return max + 1
}

// Marshal serializes the manifest to JSON.
func (m *Manifest) Marshal() ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

// UnmarshalManifest deserializes a manifest from JSON.
func UnmarshalManifest(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}
	return &m, nil
}

// SaveManifest encrypts and writes the manifest to the vault.
func SaveManifest(path string, m *Manifest, passphrase string) error {
	data, err := m.Marshal()
	if err != nil {
		return fmt.Errorf("marshaling manifest: %w", err)
	}
	return crypto.EncryptFile(path, data, passphrase)
}

// LoadManifest reads and decrypts the manifest from the vault.
func LoadManifest(path string, passphrase string) (*Manifest, error) {
	data, err := crypto.DecryptFile(path, passphrase)
	if err != nil {
		return nil, fmt.Errorf("loading manifest: %w", err)
	}
	return UnmarshalManifest(data)
}
