package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/olemoudi/kawarimi/internal/vault"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(addCmd)
	addCmd.AddCommand(addNoteCmd)
	addCmd.AddCommand(addCredentialCmd)
	addCmd.AddCommand(addDocumentCmd)
}

var addCmd = &cobra.Command{
	Use:   "add",
	Short: "Add an entry to the vault",
}

var addNoteCmd = &cobra.Command{
	Use:   "note <title>",
	Short: "Add a text note",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		title := strings.Join(args, " ")

		v, err := openVault()
		if err != nil {
			return err
		}

		content, err := editInEditor([]byte(""))
		if err != nil {
			return fmt.Errorf("editing note: %w", err)
		}

		if len(content) == 0 {
			return fmt.Errorf("empty note, nothing saved")
		}

		entry, err := v.AddNote(title, content, nil)
		if err != nil {
			return err
		}

		fmt.Printf("Added note: %s (%s)\n", entry.Title, entry.ID)
		return nil
	},
}

var addCredentialCmd = &cobra.Command{
	Use:   "credential",
	Short: "Add login credentials",
	RunE: func(cmd *cobra.Command, args []string) error {
		v, err := openVault()
		if err != nil {
			return err
		}

		reader := bufio.NewReader(os.Stdin)
		cred := &vault.Credential{}

		cred.Service = promptLine(reader, "Service name (required): ")
		if cred.Service == "" {
			return fmt.Errorf("service name is required")
		}
		cred.URL = promptLine(reader, "URL: ")
		cred.Username = promptLine(reader, "Username: ")
		cred.Password = promptLine(reader, "Password: ")
		cred.TOTPSecret = promptLine(reader, "TOTP secret: ")

		codes := promptLine(reader, "Recovery codes (comma-separated): ")
		if codes != "" {
			for _, c := range strings.Split(codes, ",") {
				c = strings.TrimSpace(c)
				if c != "" {
					cred.RecoveryCodes = append(cred.RecoveryCodes, c)
				}
			}
		}

		cred.Notes = promptLine(reader, "Notes: ")

		entry, err := v.AddCredential(cred, nil)
		if err != nil {
			return err
		}

		fmt.Printf("Added credential: %s (%s)\n", entry.Title, entry.ID)
		return nil
	},
}

var addDocumentCmd = &cobra.Command{
	Use:   "document <file>",
	Short: "Add a document file",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		filePath := args[0]

		info, err := os.Stat(filePath)
		if err != nil {
			return fmt.Errorf("reading file: %w", err)
		}

		const warnSize = 10 * 1024 * 1024 // 10MB
		if info.Size() > warnSize {
			fmt.Fprintf(os.Stderr, "WARNING: File is %.1f MB. Large files bloat git repos.\n",
				float64(info.Size())/(1024*1024))
			fmt.Fprintf(os.Stderr, "Consider using USB-only sync for large files.\n\n")
		}

		data, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("reading file: %w", err)
		}

		v, err := openVault()
		if err != nil {
			return err
		}

		reader := bufio.NewReader(os.Stdin)
		title := promptLine(reader, fmt.Sprintf("Title (default: %s): ", filepath.Base(filePath)))
		if title == "" {
			title = filepath.Base(filePath)
		}

		entry, err := v.AddDocument(title, filepath.Base(filePath), data, nil)
		if err != nil {
			return err
		}

		fmt.Printf("Added document: %s (%s)\n", entry.Title, entry.ID)
		return nil
	},
}

func promptLine(reader *bufio.Reader, prompt string) string {
	fmt.Fprint(os.Stderr, prompt)
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(line)
}

func editInEditor(initial []byte) ([]byte, error) {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	tmpDir, err := os.MkdirTemp("", "kawarimi-edit-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := os.Chmod(tmpDir, 0700); err != nil {
		return nil, fmt.Errorf("setting temp directory permissions: %w", err)
	}

	tmpFile := filepath.Join(tmpDir, "entry.md")
	if err := os.WriteFile(tmpFile, initial, 0600); err != nil {
		return nil, fmt.Errorf("writing temp file: %w", err)
	}

	editorCmd := exec.Command(editor, tmpFile)
	editorCmd.Stdin = os.Stdin
	editorCmd.Stdout = os.Stdout
	editorCmd.Stderr = os.Stderr

	if err := editorCmd.Run(); err != nil {
		return nil, fmt.Errorf("editor exited with error: %w", err)
	}

	content, err := os.ReadFile(tmpFile)
	if err != nil {
		return nil, fmt.Errorf("reading edited file: %w", err)
	}

	// Overwrite temp file with zeros before deletion
	zeros := make([]byte, len(content))
	_ = os.WriteFile(tmpFile, zeros, 0600)

	return content, nil
}

// editCredentialInEditor opens a credential as JSON in the editor.
func editCredentialInEditor(data []byte) ([]byte, error) {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	// Pretty-print the JSON for editing
	var raw json.RawMessage
	if err := json.Unmarshal(data, &raw); err == nil {
		if pretty, err := json.MarshalIndent(raw, "", "  "); err == nil {
			data = pretty
		}
	}

	tmpDir, err := os.MkdirTemp("", "kawarimi-edit-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := os.Chmod(tmpDir, 0700); err != nil {
		return nil, fmt.Errorf("setting temp directory permissions: %w", err)
	}

	tmpFile := filepath.Join(tmpDir, "entry.json")
	if err := os.WriteFile(tmpFile, data, 0600); err != nil {
		return nil, fmt.Errorf("writing temp file: %w", err)
	}

	editorCmd := exec.Command(editor, tmpFile)
	editorCmd.Stdin = os.Stdin
	editorCmd.Stdout = os.Stdout
	editorCmd.Stderr = os.Stderr

	if err := editorCmd.Run(); err != nil {
		return nil, fmt.Errorf("editor exited with error: %w", err)
	}

	content, err := os.ReadFile(tmpFile)
	if err != nil {
		return nil, fmt.Errorf("reading edited file: %w", err)
	}

	// Validate JSON
	if !json.Valid(content) {
		return nil, fmt.Errorf("edited content is not valid JSON")
	}

	// Overwrite temp file with zeros before deletion
	zeros := make([]byte, len(content))
	_ = os.WriteFile(tmpFile, zeros, 0600)

	return content, nil
}
