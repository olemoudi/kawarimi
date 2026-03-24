package crypto

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

// PromptPassphrase reads a passphrase from the terminal with echo disabled.
func PromptPassphrase(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	password, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("reading passphrase: %w", err)
	}
	return string(password), nil
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
