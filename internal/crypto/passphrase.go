package crypto

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// PromptPassphrase reads a passphrase from the terminal with echo disabled. When
// stdin is not a terminal (scripts, tests: `echo pass | kawarimi list`), it falls
// back to reading one line from stdin — the same convention age uses.
func PromptPassphrase(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		line, err := readLineUnbuffered(os.Stdin)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", fmt.Errorf("reading passphrase: %w", err)
		}
		return line, nil
	}
	password, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("reading passphrase: %w", err)
	}
	return string(password), nil
}

// readLineUnbuffered reads up to a newline one byte at a time, so it never
// consumes stdin bytes that belong to the caller's next prompt (fmt.Scanln etc.).
func readLineUnbuffered(r io.Reader) (string, error) {
	var b strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if buf[0] == '\n' {
				break
			}
			b.WriteByte(buf[0])
		}
		if err != nil {
			if err == io.EOF && b.Len() > 0 {
				break
			}
			return "", err
		}
	}
	return strings.TrimRight(b.String(), "\r"), nil
}

// PromptPassphraseConfirm prompts for a passphrase twice and returns it only if both match.
func PromptPassphraseConfirm() (string, error) {
	pass1, err := PromptPassphrase("Enter passphrase: ")
	if err != nil {
		return "", err
	}
	if pass1 == "" {
		return "", fmt.Errorf("passphrase cannot be empty")
	}
	pass2, err := PromptPassphrase("Confirm passphrase: ")
	if err != nil {
		return "", err
	}
	if pass1 != pass2 {
		return "", fmt.Errorf("passphrases do not match")
	}
	return pass1, nil
}
