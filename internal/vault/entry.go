package vault

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type Category string

const (
	CategoryNotes       Category = "notes"
	CategoryCredentials Category = "credentials"
	CategoryDocuments   Category = "documents"
)

// Entry represents a single item in the vault.
type Entry struct {
	ID          string   `json:"id"`
	Category    Category `json:"category"`
	Name        string   `json:"name"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Filename    string   `json:"filename"`
	ContentType string   `json:"content_type"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
	Tags        []string `json:"tags,omitempty"`
}

// Credential represents structured credential data before encryption.
type Credential struct {
	Service       string   `json:"service"`
	URL           string   `json:"url,omitempty"`
	Username      string   `json:"username,omitempty"`
	Password      string   `json:"password,omitempty"`
	TOTPSecret    string   `json:"totp_secret,omitempty"`
	RecoveryCodes []string `json:"recovery_codes,omitempty"`
	Notes         string   `json:"notes,omitempty"`
}

// GenerateID creates a short random hex ID.
func GenerateID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(b)
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify converts a title to a filesystem-safe slug.
func Slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 60 {
		s = s[:60]
	}
	return s
}

// NowUTC returns the current time formatted as RFC3339 in UTC.
func NowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// BuildFilename constructs the encrypted filename for a vault entry.
func BuildFilename(category Category, seq int, name string, ext string) string {
	return fmt.Sprintf("%s/%03d-%s%s.age", category, seq, name, ext)
}
