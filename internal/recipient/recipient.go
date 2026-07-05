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
	"strconv"
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
// A panic never escapes: on Windows the console window would close before the
// recipient could read anything, so a crash is explained and paused instead.
func Run(opts Options) (err error) {
	in := opts.In
	if in == nil {
		in = os.Stdin
	}
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	reader := bufio.NewReader(in)

	m := messagesFor("es") // default until the language is chosen
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(out, m.crashed, r)
			pauseOnWindows(reader, out, m)
			err = fmt.Errorf("recipient wizard crashed: %v", r)
		}
	}()

	return run(opts, reader, out, &m)
}

// run is the wizard body; Run wraps it with the crash guard. It updates *m as soon
// as the language is known so the guard speaks the chosen language.
func run(opts Options, reader *bufio.Reader, out io.Writer, mp *messages) error {
	startDir := opts.StartDir
	if startDir == "" {
		startDir, _ = os.Getwd()
	}

	lang := normalizeLang(opts.Lang)
	if lang == "" {
		lang = chooseLanguage(reader, out)
	}
	*mp = messagesFor(lang)
	m := *mp

	// Search the working directory AND the executable's directory: on macOS a
	// double-clicked binary runs with cwd = $HOME while the extracted package sits
	// next to the binary (e.g. ~/Downloads/kawarimi-vault/), so cwd-only search fails.
	exeDir := ""
	if exe, eerr := os.Executable(); eerr == nil {
		exeDir = filepath.Dir(exe)
	}
	vaultDir, baseDir, cleanup, err := locateVault([]string{startDir, exeDir})
	if err != nil {
		fmt.Fprintln(out, m.noVault)
		pauseOnWindows(reader, out, m)
		return err
	}
	if cleanup != nil {
		defer cleanup()
	}

	fmt.Fprintln(out, m.intro)
	warnIfLowMemory(out, m)

	v, err := unlockWithRetries(reader, out, m, vaultDir)
	if err != nil {
		fmt.Fprintln(out, m.gaveUp)
		pauseOnWindows(reader, out, m)
		return err
	}

	outputDir := opts.OutputDir
	if outputDir == "" {
		outputDir = filepath.Join(baseDir, "decrypted")
	}
	if err := v.Export(outputDir); err != nil {
		return fmt.Errorf("writing decrypted files: %w", err)
	}

	absOut, aerr := filepath.Abs(outputDir)
	if aerr != nil {
		absOut = outputDir
	}
	fmt.Fprintf(out, m.success, absOut)
	// Open the folder, not INDEX.md: .md file associations are unreliable (an
	// "open with?" dialog would be the recipient's very first post-decrypt
	// experience), while a folder reliably opens in Finder/Explorer/etc.
	openInFileViewer(outputDir)
	pauseOnWindows(reader, out, m)
	return nil
}

// unlockWithRetries reads the key and passphrase and tries to open the vault, up to
// maxUnlockAttempts times. A malformed key is re-prompted without spending an
// attempt, a previously accepted key can be reused by just pressing Enter, and the
// tries remaining are shown after each failure. The passphrase is normalized inside
// vault.OpenSealedV4.
func unlockWithRetries(reader *bufio.Reader, out io.Writer, m messages, vaultDir string) (*vault.Vault, error) {
	var lastKey []byte
	defer func() { crypto.ZeroBytes(lastKey) }()

	for attempt := 1; attempt <= maxUnlockAttempts; attempt++ {
		dmsKey, err := promptKey(reader, out, m, lastKey)
		if err != nil {
			return nil, errAborted
		}
		// Remember the accepted key so the next attempt can reuse it with Enter.
		crypto.ZeroBytes(lastKey)
		lastKey = append([]byte(nil), dmsKey...)

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
			fmt.Fprintf(out, m.tryAgain+"\n", maxUnlockAttempts-attempt)
		}
	}
	return nil, fmt.Errorf("could not unlock the vault after %d attempts", maxUnlockAttempts)
}

// promptKey reads key lines until one decodes, telling the user what a bad paste
// looks like each time — a typo here should never cost one of the real attempts.
// When a previous attempt already accepted a key, an empty line reuses it.
func promptKey(reader *bufio.Reader, out io.Writer, m messages, lastKey []byte) ([]byte, error) {
	prompt := m.promptKey
	if lastKey != nil {
		prompt = m.promptKeyAgain
	}
	for {
		fmt.Fprint(out, prompt)
		line, err := readLine(reader)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(line) == "" && lastKey != nil {
			return append([]byte(nil), lastKey...), nil
		}
		key, derr := crypto.DecodeDMSKeyLenient(line)
		if derr == nil {
			return key, nil
		}
		fmt.Fprintln(out, m.badKey)
	}
}

// locateVault finds the vault directory across the given search dirs, extracting a
// package zip if needed. It returns the vault dir, the base dir it was found in
// (where decrypted output should default), and a cleanup (if non-nil) that removes
// any temporary extraction dir.
func locateVault(searchDirs []string) (dir, baseDir string, cleanup func(), err error) {
	for _, d := range searchDirs {
		if d == "" {
			continue
		}
		if sub := filepath.Join(d, vault.PackageVaultDir); hasHeader(sub) {
			return sub, d, nil, nil
		}
		if hasHeader(d) {
			return d, d, nil, nil
		}
		if zipPath := vault.FindPackageZip(d); zipPath != "" {
			tmp, terr := os.MkdirTemp("", "kawarimi-open-")
			if terr != nil {
				return "", "", nil, terr
			}
			vdir, xerr := vault.ExtractPackage(zipPath, tmp)
			if xerr != nil {
				os.RemoveAll(tmp)
				return "", "", nil, xerr
			}
			return vdir, d, func() { os.RemoveAll(tmp) }, nil
		}
	}
	return "", "", nil, fmt.Errorf("no vault or package found")
}

func hasHeader(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, vault.HeaderFile))
	return err == nil
}

// availableMemoryMiB returns available RAM in MiB, or -1 if it can't be determined
// cheaply (non-Linux, or /proc unavailable). Pure Go, no CGo.
func availableMemoryMiB() int64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return -1
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemAvailable:") {
			if fields := strings.Fields(line); len(fields) >= 2 {
				if kb, perr := strconv.ParseInt(fields[1], 10, 64); perr == nil {
					return kb / 1024
				}
			}
		}
	}
	return -1
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

// warnIfLowMemory prints a friendly note if this machine likely lacks the RAM for
// the mnemonic-slot key derivation (~1 GiB). Opening the vault runs a memory-hard
// Argon2 that can OOM-crash an old low-RAM machine, so we warn before attempting.
// Best-effort: on platforms where available memory can't be read cheaply, it's a
// no-op (the printed instructions still state the requirement).
func warnIfLowMemory(out io.Writer, m messages) {
	avail := availableMemoryMiB()
	if avail >= 0 && avail < 1400 {
		fmt.Fprintf(out, m.lowMemory, avail)
	}
}

// openInFileViewer best-effort opens the decrypted folder in the OS file viewer.
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
