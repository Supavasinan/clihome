// Package update checks npm for a newer clihome release and reports whether one
// is available. Every network, filesystem, and parse failure is swallowed —
// update checking is a non-essential nicety and must never block, slow, or break
// the CLI. The newest seen version is cached in ~/.clihome/update.json and reused
// for 24h so at most one registry request is made per day.
package update

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// npmLatestURL is the npm registry endpoint for clihome's latest published
// version; the response manifest carries a top-level "version".
const npmLatestURL = "https://registry.npmjs.org/clihome/latest"

// ttl is how long a cached check is reused before re-fetching.
const ttl = 24 * time.Hour

// UpgradeCmd is what users run to update an npm-global install.
const UpgradeCmd = "npm install -g clihome@latest"

type state struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest"`
}

func cacheFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".clihome", "update.json")
}

func readCache() (state, bool) {
	f := cacheFile()
	if f == "" {
		return state{}, false
	}
	b, err := os.ReadFile(f)
	if err != nil {
		return state{}, false
	}
	var s state
	if json.Unmarshal(b, &s) != nil {
		return state{}, false
	}
	return s, true
}

func writeCache(s state) {
	f := cacheFile()
	if f == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(f), 0o755)
	if b, err := json.Marshal(s); err == nil {
		_ = os.WriteFile(f, b, 0o644)
	}
}

// fetchLatest queries the npm registry for the latest published version.
func fetchLatest() (string, bool) {
	client := &http.Client{Timeout: 2 * time.Second}
	req, err := http.NewRequest(http.MethodGet, npmLatestURL, nil)
	if err != nil {
		return "", false
	}
	// The abbreviated manifest is far smaller and still carries "version".
	req.Header.Set("Accept", "application/vnd.npm.install-v1+json")
	resp, err := client.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false
	}
	var body struct {
		Version string `json:"version"`
	}
	if json.NewDecoder(resp.Body).Decode(&body) != nil {
		return "", false
	}
	return body.Version, body.Version != ""
}

// Check returns the newest published version and whether it is newer than
// current. It reuses the cached value while fresh (< ttl) and otherwise
// refreshes from npm, persisting the result (and the attempt time, so a failed
// or offline check is not retried — paying the timeout — on every run).
func Check(current string) (latest string, newer bool) {
	cached, ok := readCache()
	if ok && time.Since(cached.CheckedAt) < ttl {
		return cached.Latest, isNewer(current, cached.Latest)
	}
	got, ok := fetchLatest()
	if !ok {
		writeCache(state{CheckedAt: time.Now(), Latest: cached.Latest})
		return cached.Latest, isNewer(current, cached.Latest)
	}
	writeCache(state{CheckedAt: time.Now(), Latest: got})
	return got, isNewer(current, got)
}

// Cached reports the newest version from the on-disk cache only — it never
// touches the network, so it is safe on the hot path of frequently-run commands.
func Cached(current string) (latest string, newer bool) {
	cached, ok := readCache()
	if !ok {
		return "", false
	}
	return cached.Latest, isNewer(current, cached.Latest)
}

// isNewer reports whether latest is a strictly higher version than current.
// Non-release current versions ("dev", "") never report an update available.
func isNewer(current, latest string) bool {
	if latest == "" || current == "" || current == "dev" {
		return false
	}
	return cmpSemver(latest, current) > 0
}

// cmpSemver compares dotted numeric versions, ignoring any pre-release/build
// suffix after '-' or '+'. Returns >0 if a>b, <0 if a<b, 0 if equal.
func cmpSemver(a, b string) int {
	pa, pb := parts(a), parts(b)
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			if x > y {
				return 1
			}
			return -1
		}
	}
	return 0
}

func parts(v string) []int {
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	fields := strings.Split(v, ".")
	out := make([]int, 0, len(fields))
	for _, f := range fields {
		n, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil {
			n = 0
		}
		out = append(out, n)
	}
	return out
}
