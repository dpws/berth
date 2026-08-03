package update

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// checkEvery is how often berth asks about releases. Often enough to hear
// about one within a day, rarely enough that starting berth twenty times in an
// afternoon is twenty starts and one request.
const checkEvery = 24 * time.Hour

// cached is what berth remembers between checks.
type cached struct {
	Tag       string `json:"tag"`
	CheckedAt int64  `json:"checked_at"`
}

// cachePath is where the last answer is kept.
func cachePath() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "berth", "latest.json")
}

// Available returns the newest release tag when it is newer than current, and
// otherwise the empty string.
//
// The answer is cached, so this is usually no request at all. A check that
// fails - no network, GitHub down, a proxy in the way - returns nothing and
// says nothing: berth is a session manager, and being unable to reach GitHub
// should cost you a notice, not an error.
func Available(ctx context.Context, current, baseURL string) string {
	if isDevBuild(current) {
		return ""
	}

	tag, fresh := readCache()
	if !fresh {
		ctx, cancel := context.WithTimeout(ctx, checkTimeout)
		defer cancel()

		rel, err := Latest(ctx, baseURL)
		if err != nil {
			return ""
		}
		tag = rel.Tag
		writeCache(tag)
	}
	if Newer(current, tag) {
		return tag
	}
	return ""
}

// readCache returns the remembered tag and whether it is recent enough to use.
func readCache() (string, bool) {
	path := cachePath()
	if path == "" {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var c cached
	if err := json.Unmarshal(data, &c); err != nil {
		return "", false
	}
	age := time.Since(time.Unix(c.CheckedAt, 0))
	return c.Tag, c.Tag != "" && age >= 0 && age < checkEvery
}

// writeCache remembers an answer. Failing to write it only costs another
// request tomorrow, so the error goes nowhere.
func writeCache(tag string) {
	path := cachePath()
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	data, err := json.Marshal(cached{Tag: tag, CheckedAt: time.Now().Unix()})
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}
