package selfupdate

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// signedRelease serves a fake GitHub release whose checksums file is signed with a
// test key, and swaps in the matching public key. Returns the asset bytes.
func signedRelease(t *testing.T, tag string, assetBody []byte, tamper func(asset, checksums, sig *[]byte)) *httptest.Server {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	orig := releasePublicKey
	releasePublicKey = pub
	t.Cleanup(func() { releasePublicKey = orig })

	name := AssetName(runtime.GOOS, runtime.GOARCH)
	sum := sha256.Sum256(assetBody)
	checksums := []byte(fmt.Sprintf("%s  %s\n%s  checksums.txt\n", hex.EncodeToString(sum[:]), name, "deadbeef"))
	sig := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, checksums)) + "\n")

	asset := append([]byte(nil), assetBody...)
	if tamper != nil {
		tamper(&asset, &checksums, &sig)
	}

	mux := http.NewServeMux()
	base := "" // set after server starts
	_ = base
	srv := httptest.NewServer(mux)
	mux.HandleFunc("/repos/olemoudi/kawarimi/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"tag_name": tag,
			"assets": []map[string]string{
				{"name": name, "browser_download_url": srv.URL + "/asset"},
				{"name": "checksums.txt", "browser_download_url": srv.URL + "/checksums"},
				{"name": "checksums.txt.sig", "browser_download_url": srv.URL + "/sig"},
			},
		})
	})
	mux.HandleFunc("/asset", func(w http.ResponseWriter, r *http.Request) { w.Write(asset) })
	mux.HandleFunc("/checksums", func(w http.ResponseWriter, r *http.Request) { w.Write(checksums) })
	mux.HandleFunc("/sig", func(w http.ResponseWriter, r *http.Request) { w.Write(sig) })

	t.Setenv("KAWARIMI_GITHUB_API", srv.URL)
	t.Cleanup(srv.Close)
	return srv
}

func TestLatestReportsNewer(t *testing.T) {
	signedRelease(t, "v0.2.0", []byte("new binary"), nil)
	rel, ok, err := Latest(context.Background(), "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || rel.Version != "0.2.0" {
		t.Fatalf("expected 0.2.0 available, got ok=%v rel=%+v", ok, rel)
	}
}

func TestLatestNoUpdateWhenSameOrOlder(t *testing.T) {
	signedRelease(t, "v0.2.0", []byte("x"), nil)
	if _, ok, _ := Latest(context.Background(), "0.2.0"); ok {
		t.Error("same version should not report an update")
	}
	if _, ok, _ := Latest(context.Background(), "0.3.0"); ok {
		t.Error("newer local version should not report an update")
	}
}

func TestLatestDevNeverUpdates(t *testing.T) {
	signedRelease(t, "v9.9.9", []byte("x"), nil)
	if _, ok, _ := Latest(context.Background(), "dev"); ok {
		t.Error("a dev build must never be told to update")
	}
}

func TestVerifyAcceptsGoodAndRejectsTampering(t *testing.T) {
	asset := []byte("the new kawarimi binary")
	// good
	srv := signedRelease(t, "v0.2.0", asset, nil)
	_ = srv
	rel, ok, err := Latest(context.Background(), "0.1.0")
	if err != nil || !ok {
		t.Fatalf("latest: %v ok=%v", err, ok)
	}
	// Re-fetch the served artifacts to run Verify directly.
	get := func(url string) []byte {
		resp, err := http.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b := make([]byte, 0)
		buf := make([]byte, 4096)
		for {
			n, e := resp.Body.Read(buf)
			b = append(b, buf[:n]...)
			if e != nil {
				break
			}
		}
		return b
	}
	checksums, sig, dl := get(rel.ChecksumURL), get(rel.SigURL), get(rel.AssetURL)
	if err := Verify(dl, checksums, sig, AssetName(runtime.GOOS, runtime.GOARCH)); err != nil {
		t.Fatalf("good artifacts should verify: %v", err)
	}

	// tampered asset (checksum mismatch)
	if err := Verify(append(dl, 'X'), checksums, sig, AssetName(runtime.GOOS, runtime.GOARCH)); err == nil {
		t.Error("a modified binary must fail checksum verification")
	}
	// tampered checksums (signature mismatch)
	bad := append([]byte(nil), checksums...)
	bad[0] ^= 0xff
	if err := Verify(dl, bad, sig, AssetName(runtime.GOOS, runtime.GOARCH)); err == nil {
		t.Error("a modified checksums file must fail signature verification")
	}
	// wrong signature
	if err := Verify(dl, checksums, []byte(base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))), AssetName(runtime.GOOS, runtime.GOARCH)); err == nil {
		t.Error("a bad signature must be rejected")
	}
}

func TestReplaceBinarySwapsInPlace(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "kawarimi")
	if err := os.WriteFile(exe, []byte("OLD"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := replaceBinary(exe, []byte("NEW")); err != nil {
		t.Fatalf("replaceBinary: %v", err)
	}
	got, _ := os.ReadFile(exe)
	if string(got) != "NEW" {
		t.Errorf("binary = %q, want NEW", got)
	}
	// no temp files left behind
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "kawarimi" && e.Name() != "kawarimi.old" {
			t.Errorf("leftover file after replace: %s", e.Name())
		}
	}
}

func TestAssetName(t *testing.T) {
	if got := AssetName("windows", "amd64"); got != "kawarimi-windows-amd64.exe" {
		t.Errorf("windows asset = %q", got)
	}
	if got := AssetName("darwin", "arm64"); got != "kawarimi-darwin-arm64" {
		t.Errorf("darwin asset = %q", got)
	}
}

// multiAssetRelease serves a fake release carrying ALL five platform binaries with
// a signed multi-line checksums file, for FetchOfficialBinaries tests. tamper can
// mutate any piece before serving.
func multiAssetRelease(t *testing.T, tag string, draft bool, tamper func(assets map[string][]byte, checksums, sig *[]byte)) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	orig := releasePublicKey
	releasePublicKey = pub
	t.Cleanup(func() { releasePublicKey = orig })

	assets := map[string][]byte{}
	var lines string
	for _, target := range officialTargets {
		name := AssetName(target[0], target[1])
		body := []byte("official binary " + name)
		assets[name] = body
		sum := sha256.Sum256(body)
		lines += fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), name)
	}
	checksums := []byte(lines)
	sig := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, checksums)) + "\n")
	if tamper != nil {
		tamper(assets, &checksums, &sig)
	}

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	mux.HandleFunc("/repos/olemoudi/kawarimi/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		list := []map[string]string{
			{"name": "checksums.txt", "browser_download_url": srv.URL + "/a/checksums.txt"},
			{"name": "checksums.txt.sig", "browser_download_url": srv.URL + "/a/checksums.txt.sig"},
		}
		for name := range assets {
			list = append(list, map[string]string{"name": name, "browser_download_url": srv.URL + "/a/" + name})
		}
		json.NewEncoder(w).Encode(map[string]any{"tag_name": tag, "draft": draft, "assets": list})
	})
	mux.HandleFunc("/a/", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Path[len("/a/"):]
		switch name {
		case "checksums.txt":
			w.Write(checksums)
		case "checksums.txt.sig":
			w.Write(sig)
		default:
			if body, ok := assets[name]; ok {
				w.Write(body)
				return
			}
			http.NotFound(w, r)
		}
	})
	t.Setenv("KAWARIMI_GITHUB_API", srv.URL)
	t.Cleanup(srv.Close)
}

func TestFetchOfficialBinariesHappyPath(t *testing.T) {
	multiAssetRelease(t, "v0.9.0", false, nil)
	dest := t.TempDir()

	tag, err := FetchOfficialBinaries(context.Background(), dest)
	if err != nil {
		t.Fatalf("FetchOfficialBinaries: %v", err)
	}
	if tag != "v0.9.0" {
		t.Errorf("tag = %q", tag)
	}
	for _, target := range officialTargets {
		name := AssetName(target[0], target[1])
		path := filepath.Join(dest, name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
		if string(data) != "official binary "+name {
			t.Errorf("%s content mismatch", name)
		}
		if runtime.GOOS != "windows" {
			if info, _ := os.Stat(path); info.Mode()&0111 == 0 {
				t.Errorf("%s is not executable", name)
			}
		}
	}
}

// One tampered binary poisons the whole fetch: nothing may be written.
func TestFetchOfficialBinariesTamperedBinaryIsAllOrNothing(t *testing.T) {
	multiAssetRelease(t, "v0.9.0", false, func(assets map[string][]byte, checksums, sig *[]byte) {
		assets[AssetName("windows", "amd64")] = []byte("evil payload")
	})
	dest := t.TempDir()

	if _, err := FetchOfficialBinaries(context.Background(), dest); err == nil {
		t.Fatal("a tampered binary must fail the whole fetch")
	}
	entries, _ := os.ReadDir(dest)
	if len(entries) != 0 {
		t.Fatalf("nothing may be written on failure, found %d files", len(entries))
	}
}

func TestFetchOfficialBinariesBadSignature(t *testing.T) {
	multiAssetRelease(t, "v0.9.0", false, func(assets map[string][]byte, checksums, sig *[]byte) {
		*sig = []byte(base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize)))
	})
	_, err := FetchOfficialBinaries(context.Background(), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("bad signature must fail loudly, got %v", err)
	}
}

func TestFetchOfficialBinariesMissingAsset(t *testing.T) {
	multiAssetRelease(t, "v0.9.0", false, func(assets map[string][]byte, checksums, sig *[]byte) {
		delete(assets, AssetName("darwin", "arm64"))
	})
	if _, err := FetchOfficialBinaries(context.Background(), t.TempDir()); err == nil {
		t.Fatal("a missing platform asset must fail the fetch")
	}
}

func TestFetchOfficialBinariesDraftRelease(t *testing.T) {
	multiAssetRelease(t, "v0.9.0", true, nil)
	if _, err := FetchOfficialBinaries(context.Background(), t.TempDir()); err == nil {
		t.Fatal("a draft release must not be used")
	}
}
