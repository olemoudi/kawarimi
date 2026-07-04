package selfupdate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/olemoudi/kawarimi/internal/atomicfile"
)

// cacheTTL is how long a cached update check is considered fresh.
const cacheTTL = 24 * time.Hour

type cacheFile struct {
	CheckedAt time.Time `json:"checked_at"`
	Available bool      `json:"available"`
	Tag       string    `json:"tag"`
	Version   string    `json:"version"`
	HTMLURL   string    `json:"html_url"`
}

func cachePath(appDir string) string { return filepath.Join(appDir, "update-check.json") }

// CachedLatest returns the last checked release if the cache says a newer version
// than current is available, plus whether the cache is still fresh. It does no
// network I/O — callers print a hint from it without slowing anything down.
func CachedLatest(appDir, current string) (rel Release, available, fresh bool) {
	data, err := os.ReadFile(cachePath(appDir))
	if err != nil {
		return Release{}, false, false
	}
	var c cacheFile
	if err := json.Unmarshal(data, &c); err != nil {
		return Release{}, false, false
	}
	fresh = time.Since(c.CheckedAt) < cacheTTL
	if !c.Available {
		return Release{}, false, fresh
	}
	// Re-check against the current version in case the binary was updated since.
	cur, ok := parseSemver(current)
	next, ok2 := parseSemver(c.Version)
	if !ok || !ok2 || !next.newerThan(cur) {
		return Release{}, false, fresh
	}
	return Release{Version: c.Version, Tag: c.Tag, HTMLURL: c.HTMLURL}, true, fresh
}

// RefreshCache performs a live check and stores the result. Errors (offline, rate
// limit) are returned but the cache is best-effort — callers can ignore them.
func RefreshCache(ctx context.Context, appDir, current string) (Release, bool, error) {
	rel, available, err := Latest(ctx, current)
	c := cacheFile{CheckedAt: time.Now(), Available: available}
	if available {
		c.Tag, c.Version, c.HTMLURL = rel.Tag, rel.Version, rel.HTMLURL
	}
	if data, mErr := json.MarshalIndent(c, "", "  "); mErr == nil {
		_ = os.MkdirAll(appDir, 0700)
		_ = atomicfile.WriteFile(cachePath(appDir), data, 0600)
	}
	return rel, available, err
}
