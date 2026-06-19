package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// modelPrice is the USD-per-token rate for a model (input / output /
// cache-write / cache-read).
type modelPrice struct{ in, out, cacheW, cacheR float64 }

func mtok(in, out, cw, cr float64) modelPrice {
	return modelPrice{in / 1e6, out / 1e6, cw / 1e6, cr / 1e6}
}

// priceFor returns per-token list prices for a model — these are an
// equivalent-value estimate; subscription users don't actually pay them.
// Prices come from models.dev (fetched + cached), falling back to a built-in
// heuristic when a model is unknown or the network is unavailable.
func priceFor(model string) modelPrice {
	m := strings.ToLower(model)
	if p, ok := prices()[m]; ok {
		return p
	}
	return priceHeuristic(m)
}

// priceHeuristic is the offline fallback when models.dev has no entry. Opus 4.5+
// is a third of the older Opus price.
func priceHeuristic(m string) modelPrice {
	switch {
	case strings.Contains(m, "opus"):
		if opusMinor(m) >= 5 {
			return mtok(5, 25, 6.25, 0.5)
		}
		return mtok(15, 75, 18.75, 1.5)
	case strings.Contains(m, "sonnet"):
		return mtok(3, 15, 3.75, 0.3)
	case strings.Contains(m, "haiku"):
		return mtok(1, 5, 1.25, 0.1)
	case strings.Contains(m, "gpt-5"), strings.Contains(m, "codex"),
		strings.Contains(m, "o3"), strings.Contains(m, "o4"):
		return mtok(1.25, 10, 0, 0.125)
	}
	return mtok(3, 15, 3.75, 0.3)
}

// opusMinor extracts N from a "...opus-4-N..." model id (0 if absent).
func opusMinor(model string) int {
	_, rest, ok := strings.Cut(model, "opus-4-")
	if !ok {
		return 0
	}
	n := 0
	for _, c := range rest {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// ── models.dev price feed (fetched once per process, cached on disk 24h) ──────

var (
	pricesOnce sync.Once
	pricesMap  map[string]modelPrice
)

func prices() map[string]modelPrice {
	pricesOnce.Do(func() { pricesMap = loadPrices() })
	return pricesMap
}

func loadPrices() map[string]modelPrice {
	if m := readPriceCache(true); m != nil { // fresh disk cache (< 24h)
		return m
	}
	if m := fetchModelsDev(); m != nil { // live fetch
		writePriceCache(m)
		return m
	}
	if m := readPriceCache(false); m != nil { // stale cache fallback (offline)
		return m
	}
	return map[string]modelPrice{} // empty → priceFor uses the heuristic
}

// fetchModelsDev pulls per-model pricing from models.dev and flattens it by
// (lowercased) model id.
func fetchModelsDev() map[string]modelPrice {
	var raw map[string]struct {
		Models map[string]struct {
			Cost struct {
				Input      float64 `json:"input"`
				Output     float64 `json:"output"`
				CacheRead  float64 `json:"cache_read"`
				CacheWrite float64 `json:"cache_write"`
			} `json:"cost"`
		} `json:"models"`
	}
	if err := httpGetJSON("https://models.dev/api.json", nil, &raw); err != nil {
		return nil
	}
	out := make(map[string]modelPrice)
	for _, prov := range raw {
		for id, m := range prov.Models {
			c := m.Cost
			if c.Input == 0 && c.Output == 0 {
				continue
			}
			out[strings.ToLower(id)] = mtok(c.Input, c.Output, c.CacheWrite, c.CacheRead)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func priceCacheFile() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".clihome", "pricing.json")
}

// readPriceCache loads the cached price map; with fresh=true it returns nil if
// the cache is older than a day.
func readPriceCache(fresh bool) map[string]modelPrice {
	f := priceCacheFile()
	fi, err := os.Stat(f)
	if err != nil {
		return nil
	}
	if fresh && time.Since(fi.ModTime()) > 24*time.Hour {
		return nil
	}
	b, err := os.ReadFile(f)
	if err != nil {
		return nil
	}
	var raw map[string][4]float64
	if json.Unmarshal(b, &raw) != nil || len(raw) == 0 {
		return nil
	}
	out := make(map[string]modelPrice, len(raw))
	for k, v := range raw {
		out[k] = modelPrice{v[0], v[1], v[2], v[3]}
	}
	return out
}

func writePriceCache(m map[string]modelPrice) {
	raw := make(map[string][4]float64, len(m))
	for k, v := range m {
		raw[k] = [4]float64{v.in, v.out, v.cacheW, v.cacheR}
	}
	f := priceCacheFile()
	_ = os.MkdirAll(filepath.Dir(f), 0o755)
	if b, err := json.Marshal(raw); err == nil {
		_ = os.WriteFile(f, b, 0o644)
	}
}
