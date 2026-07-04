// Command ksign is the release signer. goreleaser calls it to Ed25519-sign the
// checksums file with the maintainer's private key (from the RELEASE_SIGNING_KEY
// environment variable, base64 of the 32-byte seed). The matching public key is
// baked into internal/selfupdate, which verifies the signature before installing an
// update. Pure stdlib crypto — no gpg/minisign on the release runner.
//
// Usage: ksign <artifact> <signature-out>
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: ksign <artifact> <signature-out>")
		os.Exit(2)
	}
	artifact, sigOut := os.Args[1], os.Args[2]

	seedB64 := os.Getenv("RELEASE_SIGNING_KEY")
	if seedB64 == "" {
		fmt.Fprintln(os.Stderr, "ksign: RELEASE_SIGNING_KEY is not set — cannot sign the release.")
		fmt.Fprintln(os.Stderr, "Set it as a repository Actions secret (base64 of the 32-byte Ed25519 seed).")
		os.Exit(1)
	}
	seed, err := base64.StdEncoding.DecodeString(seedB64)
	if err != nil || len(seed) != ed25519.SeedSize {
		fmt.Fprintln(os.Stderr, "ksign: RELEASE_SIGNING_KEY must be base64 of a 32-byte Ed25519 seed")
		os.Exit(1)
	}
	priv := ed25519.NewKeyFromSeed(seed)

	data, err := os.ReadFile(artifact)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ksign: reading %s: %v\n", artifact, err)
		os.Exit(1)
	}
	sig := ed25519.Sign(priv, data)
	if err := os.WriteFile(sigOut, []byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "ksign: writing %s: %v\n", sigOut, err)
		os.Exit(1)
	}
}
