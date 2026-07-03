// Package recipient implements the guided, bilingual wizard a family member uses to
// open a received vault package. It deliberately uses plain stdin/stdout prompts (no
// TUI) so it behaves the same in a double-clicked console on Windows/macOS/Linux.
package recipient

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/olemoudi/kawarimi/internal/crypto"
	"github.com/olemoudi/kawarimi/internal/vault"
)

const maxUnlockAttempts = 5

var errAborted = errors.New("input closed before the vault was opened")

// Options configures a wizard run. All fields are optional; the zero value reads
// from stdin/stdout, searches the working directory, and prompts for the language.
type Options struct {
	In        io.Reader // defaults to os.Stdin
	Out       io.Writer // defaults to os.Stdout
	StartDir  string    // where to look for the vault/zip; defaults to cwd
	OutputDir string    // where to write decrypted files; defaults to <StartDir>/decrypted
	Lang      string    // "es", "en", or "" to prompt
}

// Run walks the recipient through locating, unlocking, and decrypting the vault.
func Run(opts Options) error {
	in := opts.In
	if in == nil {
		in = os.Stdin
	}
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	startDir := opts.StartDir
	if startDir == "" {
		startDir, _ = os.Getwd()
	}
	reader := bufio.NewReader(in)

	lang := normalizeLang(opts.Lang)
	if lang == "" {
		lang = chooseLanguage(reader, out)
	}
	m := messagesFor(lang)

	vaultDir, cleanup, err := locateVault(startDir)
	if err != nil {
		fmt.Fprintln(out, m.noVault)
		return err
	}
	if cleanup != nil {
		defer cleanup()
	}

	fmt.Fprintln(out, m.intro)

	v, err := unlockWithRetries(reader, out, m, vaultDir)
	if err != nil {
		fmt.Fprintln(out, m.gaveUp)
		pauseOnWindows(reader, out, m)
		return err
	}

	outputDir := opts.OutputDir
	if outputDir == "" {
		outputDir = filepath.Join(startDir, "decrypted")
	}
	if err := v.Export(outputDir); err != nil {
		return fmt.Errorf("writing decrypted files: %w", err)
	}

	absOut, aerr := filepath.Abs(outputDir)
	if aerr != nil {
		absOut = outputDir
	}
	fmt.Fprintf(out, m.success, absOut)
	openInFileViewer(filepath.Join(outputDir, "INDEX.md"))
	pauseOnWindows(reader, out, m)
	return nil
}

// unlockWithRetries reads the key and passphrase and tries to open the vault, up to
// maxUnlockAttempts times. A malformed key just costs an attempt (with a hint); the
// passphrase is normalized inside vault.OpenSealedV4.
func unlockWithRetries(reader *bufio.Reader, out io.Writer, m messages, vaultDir string) (*vault.Vault, error) {
	for attempt := 1; attempt <= maxUnlockAttempts; attempt++ {
		fmt.Fprint(out, m.promptKey)
		keyLine, err := readLine(reader)
		if err != nil {
			return nil, errAborted
		}
		dmsKey, derr := crypto.DecodeDMSKey(strings.TrimSpace(keyLine))
		if derr != nil {
			fmt.Fprintln(out, m.badKey)
			continue
		}

		fmt.Fprint(out, m.promptPass)
		passLine, err := readLine(reader)
		if err != nil {
			crypto.ZeroBytes(dmsKey)
			return nil, errAborted
		}

		fmt.Fprintln(out, m.decrypting)
		v, uerr := vault.OpenSealedV4(vaultDir, dmsKey, passLine)
		crypto.ZeroBytes(dmsKey)
		if uerr == nil {
			return v, nil
		}
		if attempt < maxUnlockAttempts {
			fmt.Fprintln(out, m.tryAgain)
		}
	}
	return nil, fmt.Errorf("could not unlock the vault after %d attempts", maxUnlockAttempts)
}

// locateVault finds the vault directory, extracting a package zip if needed. The
// returned cleanup (if non-nil) removes any temporary extraction dir.
func locateVault(startDir string) (dir string, cleanup func(), err error) {
	if sub := filepath.Join(startDir, vault.PackageVaultDir); hasHeader(sub) {
		return sub, nil, nil
	}
	if hasHeader(startDir) {
		return startDir, nil, nil
	}
	if zipPath := findPackageZip(startDir); zipPath != "" {
		tmp, err := os.MkdirTemp("", "kawarimi-open-")
		if err != nil {
			return "", nil, err
		}
		vdir, err := vault.ExtractPackage(zipPath, tmp)
		if err != nil {
			os.RemoveAll(tmp)
			return "", nil, err
		}
		return vdir, func() { os.RemoveAll(tmp) }, nil
	}
	return "", nil, fmt.Errorf("no vault or package found in %s", startDir)
}

func hasHeader(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, vault.HeaderFile))
	return err == nil
}

func findPackageZip(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".zip") {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}

func chooseLanguage(reader *bufio.Reader, out io.Writer) string {
	fmt.Fprintln(out, "Elige idioma / Choose language:")
	fmt.Fprintln(out, "  1) Español")
	fmt.Fprintln(out, "  2) English")
	fmt.Fprint(out, "> ")
	line, _ := readLine(reader)
	if strings.HasPrefix(strings.TrimSpace(line), "2") {
		return "en"
	}
	return "es" // default: the intended audience
}

func normalizeLang(l string) string {
	l = strings.ToLower(strings.TrimSpace(l))
	switch {
	case strings.HasPrefix(l, "es"):
		return "es"
	case strings.HasPrefix(l, "en"):
		return "en"
	default:
		return ""
	}
}

func readLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	line = strings.TrimRight(line, "\r\n")
	if err != nil && line == "" {
		return "", err
	}
	return line, nil
}

func pauseOnWindows(reader *bufio.Reader, out io.Writer, m messages) {
	if runtime.GOOS != "windows" {
		return
	}
	fmt.Fprint(out, m.pressEnter)
	reader.ReadString('\n')
}

// openInFileViewer best-effort opens the decrypted index in the OS file viewer.
func openInFileViewer(path string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	_ = cmd.Start() // best effort; ignore errors (no viewer, headless, etc.)
}
