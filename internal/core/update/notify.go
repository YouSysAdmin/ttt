package update

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// The CLI update notice must never make everyday commands feel the network:
// GitHub is asked at most once per notifyTTL, the answer is cached in the
// user cache dir, and the one request that does happen gets a short timeout.
const notifyTTL = 24 * time.Hour

var notifyClient = &http.Client{Timeout: 3 * time.Second}

// userCacheDir is a var so tests can redirect the cache.
var userCacheDir = os.UserCacheDir

type checkCache struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest"`
}

// NotifyIfOutdated prints a one-line update notice to w when a newer release
// exists, with the right upgrade command for the install source. Dev builds
// and all errors stay silent — a notice is never worth failing or delaying a
// command for.
func NotifyIfOutdated(currentVersion string, w io.Writer) {
	if isDev(currentVersion) {
		return
	}

	latest, ok := cachedLatest()
	if !ok {
		rel, err := fetchReleaseWith(notifyClient, ReleaseAPIURL)
		if err != nil {
			return
		}
		latest = stripV(rel.TagName)
		writeCache(latest)
	}

	if latest == "" || CompareVersions(currentVersion, latest) >= 0 {
		return
	}

	hint := "ttt update"
	if src, err := DetectSource(); err == nil && src.Managed() {
		hint = src.UpdateHint()
	}
	fmt.Fprintf(w, "\nUpdate available: v%s -> v%s — run: %s\n", currentVersion, latest, hint)
}

func cachePath() (string, error) {
	dir, err := userCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ttt", "update-check.json"), nil
}

// cachedLatest returns the cached latest version when the cache is fresh.
func cachedLatest() (string, bool) {
	path, err := cachePath()
	if err != nil {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var c checkCache
	if err := json.Unmarshal(data, &c); err != nil {
		return "", false
	}
	if time.Since(c.CheckedAt) >= notifyTTL {
		return "", false
	}
	return c.Latest, true
}

func writeCache(latest string) {
	path, err := cachePath()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	data, err := json.Marshal(checkCache{CheckedAt: time.Now(), Latest: latest})
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}
