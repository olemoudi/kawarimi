package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/olemoudi/kawarimi/internal/config"
	"github.com/olemoudi/kawarimi/internal/crypto"
	"github.com/olemoudi/kawarimi/internal/vault"
	"github.com/spf13/cobra"
)

var verifyRecipient bool

func init() {
	verifyCmd.Flags().BoolVar(&verifyRecipient, "recipient", false,
		"Prove a recipient could open the vault: unseal with the local DMS key + the card passphrase")
	rootCmd.AddCommand(verifyCmd)
}

var verifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Verify all vault entries can be decrypted",
	RunE: func(cmd *cobra.Command, args []string) error {
		if verifyRecipient {
			return verifyRecipientPath()
		}

		v, err := openVault()
		if err != nil {
			return err
		}

		errs := v.Verify()
		if len(errs) == 0 {
			fmt.Printf("All %d entries verified OK\n", len(v.Manifest.Entries))
			return nil
		}

		for _, e := range errs {
			fmt.Printf("FAIL: %s\n", e)
		}
		return fmt.Errorf("%d of %d entries failed verification", len(errs), len(v.Manifest.Entries))
	},
}

// verifyRecipientPath runs the exact path a recipient will use post-mortem — unseal
// the payload with the DMS key + the card passphrase — so the owner can prove today
// that it works, instead of the recipient discovering a problem when it is too late.
// It needs the local DMS key, which exists right after init/rekey and in local-release
// mode (in cloud-only mode it is removed for security; run this before that, or rekey).
func verifyRecipientPath() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	appDir, err := config.AppDirPath()
	if err != nil {
		return err
	}
	dmsKeyData, err := os.ReadFile(filepath.Join(appDir, "dms-key"))
	if err != nil {
		return fmt.Errorf("the recipient self-check needs the local DMS key, which is not present here " +
			"(it is removed in cloud-only mode after setup) — run this right after 'init'/'switch rekey', " +
			"or temporarily regenerate it with 'switch rekey'")
	}
	dmsKey, err := crypto.DecodeDMSKeyLenient(string(dmsKeyData))
	if err != nil {
		return fmt.Errorf("decoding local DMS key: %w", err)
	}
	defer crypto.ZeroBytes(dmsKey)

	passphrase := promptLine(bufio.NewReader(os.Stdin), "Recipient passphrase from the physical card: ")
	if strings.TrimSpace(passphrase) == "" {
		return fmt.Errorf("recipient passphrase is required")
	}

	v, err := vault.OpenSealedV4(cfg.VaultDir, dmsKey, passphrase)
	if err != nil {
		return fmt.Errorf("RECIPIENT SELF-CHECK FAILED — the DMS key + card passphrase do NOT open the vault: %w", err)
	}
	fmt.Printf("Recipient self-check PASSED: the DMS key + card passphrase open the vault (%d entries).\n", len(v.Manifest.Entries))
	return nil
}
