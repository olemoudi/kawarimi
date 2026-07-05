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

// PromptNewPassphrase prompts (with confirmation) for a NEW vault-protecting
// passphrase, shows the strength meter, and gates weak choices: an interactive
// user must explicitly accept a below-fair password (or re-enter a better one).
// Non-interactive input only warns, so scripts and tests keep working; setting
// KAWARIMI_STRENGTH_STRICT=1 forces the gate even without a terminal (used by
// tests to exercise both gate outcomes).
func PromptNewPassphrase() (string, error) {
	for {
		pass, err := PromptPassphraseConfirm()
		if err != nil {
			return "", err
		}
		strength := EstimatePasswordStrength(pass)
		fmt.Fprintln(os.Stderr, RenderStrengthMeter(strength))
		if strength.Level >= AcceptableStrengthLevel {
			return pass, nil
		}
		interactive := term.IsTerminal(int(os.Stdin.Fd()))
		if !interactive && os.Getenv("KAWARIMI_STRENGTH_STRICT") != "1" {
			fmt.Fprintln(os.Stderr, "WARNING: proceeding with a weak password (non-interactive input).")
			return pass, nil
		}
		fmt.Fprint(os.Stderr, "This password is below the recommended strength. Use it anyway? [y/N]: ")
		answer, err := readLineUnbuffered(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("reading confirmation: %w", err)
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "y", "yes":
			return pass, nil
		}
		fmt.Fprintln(os.Stderr, "Pick a stronger passphrase (a few random words, or 12+ mixed characters).")
	}
}

// RenderStrengthMeter renders a one-line ASCII strength meter for CLI display.
func RenderStrengthMeter(s PasswordStrength) string {
	filled := (s.Level + 1) * 2
	bar := strings.Repeat("#", filled) + strings.Repeat("-", 10-filled)
	label := strings.ReplaceAll(s.LevelKey, "_", " ")
	return fmt.Sprintf("Strength: [%s] %s (~%.0f bits) — est. %s to crack for a $100k/year attacker",
		bar, label, s.Bits, FormatCrackTime(s.CrackYears))
}

// FormatCrackTime renders an expected crack time (in years) as a rough
// human-readable duration.
func FormatCrackTime(years float64) string {
	switch {
	case years < 1.0/8766: // under an hour
		return "less than an hour"
	case years < 2.0/365:
		return fmt.Sprintf("%.0f hours", years*8766)
	case years < 2.0/12:
		return fmt.Sprintf("%.0f days", years*365.25)
	case years < 2:
		return fmt.Sprintf("%.0f months", years*12)
	case years < 1000:
		return fmt.Sprintf("%.0f years", years)
	case years < 1e6:
		return fmt.Sprintf("%.0f thousand years", years/1e3)
	case years < 1e9:
		return fmt.Sprintf("%.0f million years", years/1e6)
	default:
		return "over a billion years"
	}
}
