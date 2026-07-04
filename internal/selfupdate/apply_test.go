package selfupdate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// applyFixture returns a Release from the signedRelease server plus a fake
// installed binary to update over.
func applyFixture(t *testing.T, assetBody []byte, tamper func(asset, checksums, sig *[]byte)) (Release, string) {
	t.Helper()
	signedRelease(t, "v9.9.9", assetBody, tamper)
	rel, ok, err := Latest(context.Background(), "0.1.0")
	if err != nil || !ok {
		t.Fatalf("latest: %v ok=%v", err, ok)
	}
	exe := filepath.Join(t.TempDir(), "kawarimi")
	if err := os.WriteFile(exe, []byte("OLD BINARY"), 0755); err != nil {
		t.Fatal(err)
	}
	return rel, exe
}

func TestApplyInstallsVerifiedUpdate(t *testing.T) {
	newBinary := []byte("THE NEW KAWARIMI BINARY")
	rel, exe := applyFixture(t, newBinary, nil)

	if err := Apply(context.Background(), rel, exe); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(newBinary) {
		t.Errorf("installed binary = %q, want the release asset", got)
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(exe)
		if info.Mode().Perm() != 0755 {
			t.Errorf("installed binary mode = %v, want 0755", info.Mode().Perm())
		}
	}
}

func TestApplyRejectsTamperedAsset(t *testing.T) {
	rel, exe := applyFixture(t, []byte("legit"), func(asset, _, _ *[]byte) {
		*asset = append(*asset, "MALWARE"...)
	})
	err := Apply(context.Background(), rel, exe)
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("tampered asset must fail the checksum, got %v", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "OLD BINARY" {
		t.Error("a failed update must not touch the installed binary")
	}
}

func TestApplyRejectsTamperedChecksums(t *testing.T) {
	rel, exe := applyFixture(t, []byte("legit"), func(_, checksums, _ *[]byte) {
		(*checksums)[0] ^= 0xff
	})
	err := Apply(context.Background(), rel, exe)
	if err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("tampered checksums must fail the signature, got %v", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "OLD BINARY" {
		t.Error("a failed update must not touch the installed binary")
	}
}

func TestApplyReportsDownloadFailure(t *testing.T) {
	rel, exe := applyFixture(t, []byte("legit"), nil)
	rel.AssetURL += "-missing" // 404
	if err := Apply(context.Background(), rel, exe); err == nil {
		t.Fatal("a failed download must fail the update")
	}
	if got, _ := os.ReadFile(exe); string(got) != "OLD BINARY" {
		t.Error("a failed update must not touch the installed binary")
	}
}

func TestCacheRoundTrip(t *testing.T) {
	signedRelease(t, "v2.0.0", []byte("bin"), nil)
	appDir := t.TempDir()

	// No cache yet: nothing available, nothing fresh.
	if _, avail, fresh := CachedLatest(appDir, "1.0.0"); avail || fresh {
		t.Errorf("empty cache: avail=%v fresh=%v, want false/false", avail, fresh)
	}

	// Live refresh populates the cache.
	rel, avail, err := RefreshCache(context.Background(), appDir, "1.0.0")
	if err != nil || !avail || rel.Version != "2.0.0" {
		t.Fatalf("RefreshCache: rel=%+v avail=%v err=%v", rel, avail, err)
	}

	// Cache now reports the update without any network.
	rel, avail, fresh := CachedLatest(appDir, "1.0.0")
	if !avail || !fresh || rel.Version != "2.0.0" {
		t.Errorf("cached: rel=%+v avail=%v fresh=%v", rel, avail, fresh)
	}

	// After the binary catches up, the same cache reports nothing.
	if _, avail, _ := CachedLatest(appDir, "2.0.0"); avail {
		t.Error("cache must not offer an update the binary already has")
	}
	if _, avail, _ := CachedLatest(appDir, "3.0.0"); avail {
		t.Error("cache must not offer a downgrade")
	}
}

func TestCacheGoesStale(t *testing.T) {
	appDir := t.TempDir()
	stale := cacheFile{CheckedAt: time.Now().Add(-2 * cacheTTL), Available: true, Tag: "v2.0.0", Version: "2.0.0"}
	data, _ := json.Marshal(stale)
	if err := os.WriteFile(cachePath(appDir), data, 0600); err != nil {
		t.Fatal(err)
	}
	rel, avail, fresh := CachedLatest(appDir, "1.0.0")
	if !avail || rel.Version != "2.0.0" {
		t.Errorf("stale cache should still report the release: %+v avail=%v", rel, avail)
	}
	if fresh {
		t.Error("a cache older than the TTL must not be fresh")
	}
}

func TestCacheIgnoresCorruptFile(t *testing.T) {
	appDir := t.TempDir()
	if err := os.WriteFile(cachePath(appDir), []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, avail, fresh := CachedLatest(appDir, "1.0.0"); avail || fresh {
		t.Error("a corrupt cache must read as no update, not an error")
	}
}

func TestLatestErrorPaths(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	t.Setenv("KAWARIMI_GITHUB_API", srv.URL)

	status := http.StatusNotFound
	var body map[string]any
	mux.HandleFunc("/repos/olemoudi/kawarimi/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		json.NewEncoder(w).Encode(body)
	})

	// 404: no release published yet — silent, no error.
	if _, ok, err := Latest(context.Background(), "0.1.0"); ok || err != nil {
		t.Errorf("404: ok=%v err=%v, want silent no-update", ok, err)
	}

	// 500: an error the caller can choose to ignore.
	status = http.StatusInternalServerError
	if _, _, err := Latest(context.Background(), "0.1.0"); err == nil {
		t.Error("HTTP 500 must surface as an error")
	}

	// Draft and prerelease releases are never offered.
	status = http.StatusOK
	body = map[string]any{"tag_name": "v9.9.9", "draft": true}
	if _, ok, _ := Latest(context.Background(), "0.1.0"); ok {
		t.Error("a draft release must not be offered")
	}
	body = map[string]any{"tag_name": "v9.9.9", "prerelease": true}
	if _, ok, _ := Latest(context.Background(), "0.1.0"); ok {
		t.Error("a prerelease must not be offered")
	}

	// A newer release missing this platform's asset is an explicit error.
	body = map[string]any{"tag_name": "v9.9.9", "assets": []map[string]string{}}
	if _, _, err := Latest(context.Background(), "0.1.0"); err == nil {
		t.Error("a release without our asset/checksums/sig must error")
	}
}

func TestParseSemver(t *testing.T) {
	cases := []struct {
		in    string
		want  semver
		valid bool
	}{
		{"1.2.3", semver{1, 2, 3}, true},
		{"v1.2.3", semver{1, 2, 3}, true},
		{" v0.10.0 ", semver{0, 10, 0}, true},
		{"1.2.3-rc1", semver{1, 2, 3}, true},
		{"1.2.3+build5", semver{1, 2, 3}, true},
		{"dev", semver{}, false},
		{"", semver{}, false},
		{"1.2", semver{}, false},
		{"1.2.x", semver{}, false},
		{"1.-2.3", semver{}, false},
	}
	for _, c := range cases {
		got, ok := parseSemver(c.in)
		if ok != c.valid || (ok && got != c.want) {
			t.Errorf("parseSemver(%q) = %+v/%v, want %+v/%v", c.in, got, ok, c.want, c.valid)
		}
	}
	if !(semver{1, 0, 0}).newerThan(semver{0, 9, 9}) ||
		!(semver{0, 2, 0}).newerThan(semver{0, 1, 9}) ||
		!(semver{0, 0, 2}).newerThan(semver{0, 0, 1}) ||
		(semver{1, 2, 3}).newerThan(semver{1, 2, 3}) {
		t.Error("newerThan ordering is wrong")
	}
}

func TestCleanupOldIsSafeEverywhere(t *testing.T) {
	// On non-Windows it is a no-op; on Windows it removes <exe>.old. Either way
	// it must never panic or error on a normal startup.
	CleanupOld()
}
