// Package selfupdate updates the OWNER's kawarimi binary from signed GitHub
// releases. Every download is verified twice: an Ed25519 signature over the
// checksums file (against a public key baked into the binary — an attacker who
// compromises the GitHub account still cannot forge an update without the private
// key), then a SHA-256 of the downloaded binary against that verified checksums
// file. It is pure Go / stdlib crypto, so it adds no dependency and keeps the
// single-binary distribution intact.
//
// This is owner-only. The recipient path must never call it: the recipient binary
// is frozen by design so it can open the vault years later, offline.
package selfupdate

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// releasePublicKeyB64 is the Ed25519 public key that release signatures are checked
// against. The matching private key is held only by the maintainer (a GitHub Actions
// secret). Rotating it means shipping a new key in a signed update.
const releasePublicKeyB64 = "6xlW8p0JSEdXNwNCASMj0M3UlJyeFoOd5PnAueRLmwI="

const (
	repoSlug         = "olemoudi/kawarimi"
	maxDownloadBytes = 150 << 20 // generous cap for a static binary
)

// releasePublicKey is a var (not a const-derived literal) so tests can substitute a
// test key. Production always uses the baked key above.
var releasePublicKey = mustDecodeKey(releasePublicKeyB64)

func mustDecodeKey(b64 string) ed25519.PublicKey {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		panic("selfupdate: invalid baked public key")
	}
	return ed25519.PublicKey(raw)
}

// apiBase returns the GitHub API base, honoring KAWARIMI_GITHUB_API for tests
// (same override the internal/github client uses).
func apiBase() string {
	if v := os.Getenv("KAWARIMI_GITHUB_API"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "https://api.github.com"
}

func httpClient() *http.Client { return &http.Client{Timeout: 90 * time.Second} }

// Release is the newest published release relevant to this platform.
type Release struct {
	Version     string // e.g. "0.2.0" (no leading v)
	Tag         string // e.g. "v0.2.0"
	Notes       string
	HTMLURL     string
	AssetURL    string // the kawarimi binary for this os/arch
	ChecksumURL string // checksums.txt
	SigURL      string // checksums.txt.sig
}

// AssetName is the release asset for the given platform.
func AssetName(goos, goarch string) string {
	name := fmt.Sprintf("kawarimi-%s-%s", goos, goarch)
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

type ghRelease struct {
	TagName    string `json:"tag_name"`
	Body       string `json:"body"`
	HTMLURL    string `json:"html_url"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// Latest reports whether a newer release than currentVersion is available. A "dev"
// or unparseable current version never reports an update (source builds shouldn't
// be nagged). Network errors are returned so callers can stay silent on them.
func Latest(ctx context.Context, currentVersion string) (rel Release, available bool, err error) {
	cur, ok := parseSemver(currentVersion)
	if !ok {
		return Release{}, false, nil // dev / unknown build — never offer an update
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, apiBase()+"/repos/"+repoSlug+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := httpClient().Do(req)
	if err != nil {
		return Release{}, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return Release{}, false, nil // no published release yet
	}
	if resp.StatusCode != http.StatusOK {
		return Release{}, false, fmt.Errorf("release check: HTTP %d", resp.StatusCode)
	}

	var gr ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&gr); err != nil {
		return Release{}, false, err
	}
	if gr.Draft || gr.Prerelease {
		return Release{}, false, nil
	}
	next, ok := parseSemver(gr.TagName)
	if !ok || !next.newerThan(cur) {
		return Release{}, false, nil
	}

	rel = Release{Version: strings.TrimPrefix(gr.TagName, "v"), Tag: gr.TagName, Notes: gr.Body, HTMLURL: gr.HTMLURL}
	want := AssetName(runtime.GOOS, runtime.GOARCH)
	for _, a := range gr.Assets {
		switch a.Name {
		case want:
			rel.AssetURL = a.URL
		case "checksums.txt":
			rel.ChecksumURL = a.URL
		case "checksums.txt.sig":
			rel.SigURL = a.URL
		}
	}
	if rel.AssetURL == "" || rel.ChecksumURL == "" || rel.SigURL == "" {
		return Release{}, false, fmt.Errorf("release %s is missing the binary, checksums, or signature for %s", gr.TagName, want)
	}
	return rel, true, nil
}

// Apply downloads, verifies, and installs rel over exePath. It returns only after
// the running binary has been replaced; the caller must instruct the user to
// restart. exePath defaults to os.Executable() when empty.
func Apply(ctx context.Context, rel Release) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating current binary: %w", err)
	}
	exePath, _ = filepath.EvalSymlinks(exePath)

	checksums, err := download(ctx, rel.ChecksumURL)
	if err != nil {
		return fmt.Errorf("downloading checksums: %w", err)
	}
	sigRaw, err := download(ctx, rel.SigURL)
	if err != nil {
		return fmt.Errorf("downloading signature: %w", err)
	}
	asset, err := download(ctx, rel.AssetURL)
	if err != nil {
		return fmt.Errorf("downloading update: %w", err)
	}

	if err := Verify(asset, checksums, sigRaw, AssetName(runtime.GOOS, runtime.GOARCH)); err != nil {
		return err
	}
	return replaceBinary(exePath, asset)
}

// Verify checks the Ed25519 signature over the checksums file and then the asset's
// SHA-256 against its line in that (now-trusted) file. Any mismatch is fatal.
func Verify(asset, checksums, sigEncoded []byte, assetName string) error {
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigEncoded)))
	if err != nil {
		return fmt.Errorf("decoding signature: %w", err)
	}
	if len(sig) != ed25519.SignatureSize || !ed25519.Verify(releasePublicKey, checksums, sig) {
		return fmt.Errorf("the update's signature is not valid — refusing to install (possible tampering)")
	}

	want, err := checksumFor(checksums, assetName)
	if err != nil {
		return err
	}
	got := sha256.Sum256(asset)
	if hex.EncodeToString(got[:]) != want {
		return fmt.Errorf("the downloaded update does not match its checksum — refusing to install")
	}
	return nil
}

// checksumFor finds assetName's hex SHA-256 in a goreleaser-style checksums file
// ("<hex>  <name>" per line).
func checksumFor(checksums []byte, assetName string) (string, error) {
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == assetName {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("no checksum for %s in the release", assetName)
}

func download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxDownloadBytes))
}

// --- semver (tiny, no dependency) ---

type semver struct{ major, minor, patch int }

func parseSemver(s string) (semver, bool) {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "v"))
	// drop any pre-release / build metadata
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	var v semver
	for i, dst := range []*int{&v.major, &v.minor, &v.patch} {
		n, err := strconv.Atoi(parts[i])
		if err != nil || n < 0 {
			return semver{}, false
		}
		*dst = n
	}
	return v, true
}

func (a semver) newerThan(b semver) bool {
	if a.major != b.major {
		return a.major > b.major
	}
	if a.minor != b.minor {
		return a.minor > b.minor
	}
	return a.patch > b.patch
}
