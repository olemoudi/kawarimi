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

const readmeContent = `# Important Information Vault

This directory contains encrypted end-of-life information.

If you are reading this, you likely have or will receive a passphrase
to decrypt these files.

## How to Decrypt

See DECRYPT_INSTRUCTIONS.md for step-by-step instructions.

## Quick Version

Install age: https://github.com/FiloSottile/age/releases
Then run:

    age -d manifest.age > manifest.json

The manifest.json file lists all encrypted files and what they contain.
Then decrypt any file the same way:

    age -d notes/001-bank-accounts.md.age > bank-accounts.md

## What's Inside

- notes/       - Written instructions and information (Markdown)
- credentials/ - Account credentials (JSON)
- documents/   - Scanned documents (PDF, images)
`

const decryptInstructionsContent = `# How to Decrypt This Vault

## Prerequisites

- The decryption passphrase
- The 'age' tool (https://github.com/FiloSottile/age/releases)

## Step-by-Step

### 1. Install age

- **macOS**: brew install age
- **Linux**: Download from GitHub releases page
- **Windows**: Download .exe from GitHub releases page

### 2. Decrypt the manifest

    age -d manifest.age > manifest.json

Enter the passphrase when prompted.
Open manifest.json in any text editor to see the list of all files.

### 3. Decrypt individual files

    age -d notes/001-bank-accounts.md.age > bank-accounts.md
    age -d credentials/001-email-google.json.age > email-google.json
    age -d documents/001-will-scan.pdf.age > will-scan.pdf

### 4. Decrypt everything at once (bash/zsh)

    mkdir decrypted
    for f in $(find . -name '*.age' -not -name 'manifest.age'); do
      out="decrypted/$(echo $f | sed 's/.age$//')"
      mkdir -p "$(dirname "$out")"
      age -d "$f" > "$out"
    done

### 5. Understanding the contents

- notes/       - Written instructions in Markdown (open with any text editor)
- credentials/ - Account credentials in JSON format (open with any text editor)
- documents/   - Scanned documents (use PDF viewer, image viewer, etc.)

### If You Have the Kawarimi Tool

    kawarimi export ./decrypted

This will decrypt everything and generate a readable index.
`
