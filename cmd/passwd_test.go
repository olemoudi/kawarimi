package cmd

import (
	"path/filepath"
	"testing"

	"github.com/olemoudi/kawarimi/internal/config"
	"github.com/olemoudi/kawarimi/internal/crypto"
	"github.com/olemoudi/kawarimi/internal/testenv"
	"github.com/olemoudi/kawarimi/internal/vault"
)

// The rekey path: after `kawarimi passwd`, the new password must open the vault,
// the old one must not, and the recovery slot must be re-bound to the new password
// (a half-updated header would lock the owner out of their own recovery path).
func TestPasswdRotatesEverySlot(t *testing.T) {
	env := testenv.New(t)
	secrets := env.InitVault(t)
	cfg := env.Config(t)
	const newPassword = "brand-new-password-42"

	// current password, then the new one twice (confirm).
	withStdin(t, env.Password()+"\n"+newPassword+"\n"+newPassword+"\n")
	if err := passwdV2(cfg); err != nil {
		t.Fatalf("passwdV2: %v", err)
	}

	// New password opens the owner slot.
	withStdin(t, newPassword+"\n")
	if _, err := openVault(); err != nil {
		t.Fatalf("new password must open the vault: %v", err)
	}

	// Old password no longer works.
	withStdin(t, env.Password()+"\n")
	if _, err := openVault(); err == nil {
		t.Fatal("old password must stop working after passwd")
	}

	// The recovery slot was rebound to the new password.
	header, err := vault.LoadHeader(cfg.VaultDir)
	if err != nil {
		t.Fatal(err)
	}
	recoveryCode, err := crypto.DecodeRecoveryCode(secrets.RecoveryCode)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := header.OpenWithRecovery(newPassword, recoveryCode); err != nil {
		t.Fatalf("recovery code + new password must open the vault: %v", err)
	}
	if _, _, err := header.OpenWithRecovery(env.Password(), recoveryCode); err == nil {
		t.Fatal("recovery code + old password must stop working after passwd")
	}

	// The mnemonic (paper backup) is deliberately unaffected by a password change.
	if _, _, err := header.OpenWithMnemonic(secrets.MnemonicWords); err != nil {
		t.Fatalf("mnemonic must survive a password change: %v", err)
	}

	// A wrong current password must not touch anything.
	withStdin(t, "not-the-password\nx\nx\n")
	if err := passwdV2(cfg); err == nil {
		t.Fatal("passwd with a wrong current password must fail")
	}
}

// The legacy v1 path: `kawarimi passwd` on a passphrase-only vault must
// re-encrypt every entry + the manifest under the new passphrase.
func TestPasswdV1ReencryptsVault(t *testing.T) {
	home := testenv.SetHome(t, t.TempDir())
	dir := filepath.Join(home, "v1-vault")

	v1, err := vault.Create(dir, "old passphrase")
	if err != nil {
		t.Fatalf("Create v1: %v", err)
	}
	if _, err := v1.AddNote("Bank", []byte("acct 42"), nil); err != nil {
		t.Fatalf("AddNote: %v", err)
	}

	cfg := config.DefaultConfig(dir)
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}

	// current passphrase, then the new one twice (confirm).
	withStdin(t, "old passphrase\nnew passphrase 42\nnew passphrase 42\n")
	if err := passwdV1(cfg); err != nil {
		t.Fatalf("passwdV1: %v", err)
	}

	// New passphrase decrypts the manifest and the entry; the old one must not.
	if ok, err := openVaultWithPassphrase(dir, "new passphrase 42"); !ok {
		t.Fatalf("new passphrase must open the v1 vault: %v", err)
	}
	if ok, _ := openVaultWithPassphrase(dir, "old passphrase"); ok {
		t.Error("the old passphrase must not open the vault after passwd")
	}

	v, err := vault.Open(dir, "new passphrase 42")
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Manifest.Entries) != 1 {
		t.Fatalf("expected 1 entry after rekey, got %d", len(v.Manifest.Entries))
	}
	data, err := v.ShowEntry(v.Manifest.Entries[0])
	if err != nil || string(data) != "acct 42" {
		t.Errorf("entry content after rekey = %q, %v", data, err)
	}
}
