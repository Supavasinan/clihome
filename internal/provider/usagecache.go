package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const (
	usageTTL   = 60 * time.Second // serve the disk cache without re-fetching
	usageStale = 30 * time.Minute // how stale a cached value may be on error
)

type usageCacheEntry struct {
	FetchedAt int64  `json:"at"`
	Usage     *Usage `json:"usage"`
}

func usageCacheFile(key string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".clihome", "usage", key+".json")
}

// cachedUsage serves a fresh on-disk cache without calling the API, fetches live
// when stale, and falls back to the last-known value on error — so a rate-limit
// (HTTP 429) shows recent usage instead of failing.
func cachedUsage(key string, fetch func() (*Usage, error)) (*Usage, error) {
	cf := usageCacheFile(key)
	cached, ts := readUsageCache(cf)
	age := time.Since(time.Unix(ts, 0))
	if cached != nil && age < usageTTL {
		return cached, nil
	}
	u, err := fetch()
	if err != nil {
		if cached != nil && age < usageStale {
			return cached, nil
		}
		return nil, err
	}
	writeUsageCache(cf, u)
	return u, nil
}

func readUsageCache(f string) (*Usage, int64) {
	b, err := os.ReadFile(f)
	if err != nil {
		return nil, 0
	}
	var e usageCacheEntry
	if json.Unmarshal(b, &e) != nil {
		return nil, 0
	}
	return e.Usage, e.FetchedAt
}

func writeUsageCache(f string, u *Usage) {
	_ = os.MkdirAll(filepath.Dir(f), 0o755)
	if b, err := json.Marshal(usageCacheEntry{FetchedAt: time.Now().Unix(), Usage: u}); err == nil {
		_ = os.WriteFile(f, b, 0o644)
	}
}
