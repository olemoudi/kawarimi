package vault

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/olemoudi/kawarimi/internal/crypto"
)

// OpenSealedV4 opens a V4 vault from its directory using the DMS key and the
// recipient passphrase. It reads the sealed payload from the vault dir, recovers
// the mnemonic, and unlocks the vault. Shared by `export --sealed` and the recipient
// wizard so both use one implementation. The passphrase is normalized, so a card
// typed with stray capitals or spaces still matches.
func OpenSealedV4(vaultDir string, dmsKey []byte, passphrase string) (*Vault, error) {
	header, err := LoadHeader(vaultDir)
	if err != nil {
		return nil, fmt.Errorf("loading vault header: %w", err)
	}

	ciphertext, err := os.ReadFile(filepath.Join(vaultDir, SealedPayloadFile))
	if err != nil {
		return nil, fmt.Errorf("reading sealed payload: %w", err)
	}

	entropy, err := crypto.UnsealMnemonicV4(ciphertext, dmsKey, crypto.NormalizeRecipientPassphrase(passphrase))
	if err != nil {
		return nil, err
	}
	defer crypto.ZeroBytes(entropy)

	words, err := crypto.EncodeMnemonic(entropy)
	if err != nil {
		return nil, fmt.Errorf("encoding mnemonic: %w", err)
	}

	_, ageIdentity, err := header.OpenWithMnemonic(words)
	if err != nil {
		return nil, fmt.Errorf("unlocking vault: %w", err)
	}

	return OpenV2(vaultDir, ageIdentity, header.AgeRecipient)
}
