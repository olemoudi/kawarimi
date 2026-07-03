package vault

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

func marshalJSON(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

func contentTypeFromExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".pdf":
		return "application/pdf"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".txt":
		return "text/plain"
	default:
		return "application/octet-stream"
	}
}

func generateIndex(m *Manifest) string {
	var sb strings.Builder
	sb.WriteString("# Vault Contents\n\n")
	sb.WriteString(fmt.Sprintf("Exported on: %s\n\n", NowUTC()))

	categories := []struct {
		cat   Category
		title string
	}{
		{CategoryNotes, "Notes"},
		{CategoryCredentials, "Credentials"},
		{CategoryDocuments, "Documents"},
	}

	for _, c := range categories {
		entries := m.FindEntriesByCategory(c.cat)
		if len(entries) == 0 {
			continue
		}
		sb.WriteString(fmt.Sprintf("## %s\n\n", c.title))
		for i, e := range entries {
			// Strip .age extension for display
			displayFile := e.Filename
			if len(displayFile) > 4 && displayFile[len(displayFile)-4:] == ".age" {
				displayFile = displayFile[:len(displayFile)-4]
			}
			sb.WriteString(fmt.Sprintf("%d. **%s**", i+1, e.Title))
			if e.Description != "" {
				sb.WriteString(fmt.Sprintf(" — %s", e.Description))
			}
			sb.WriteString(fmt.Sprintf("\n   File: %s\n\n", filepath.Base(displayFile)))
		}
	}

	return sb.String()
}

// The recipient-facing README and decrypt instructions now live in
// internal/copytext (bilingual, and correct for V2/V4 vaults). See
// writeVaultReadme in vault.go.
