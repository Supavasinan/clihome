// Package tokens holds the shared token/cost types. Each provider aggregates
// its own transcripts (the per-tool dedup and on-disk formats differ), so the
// walking lives in the provider files; this package just defines the result
// shape and a day boundary.
package tokens

import "time"

// Stat is a token count and its estimated USD cost (equivalent API value).
type Stat struct {
	Tokens int64
	Cost   float64
}

// Usage is a home's today + all-time token usage, plus a per-day series
// (date "2006-01-02" → tokens+cost) that drives the activity heatmap and its
// day breakdown.
type Usage struct {
	Today Stat
	Total Stat
	Daily map[string]Stat
}

// Session is one transcript's token + cost rollup, for the session-history view.
type Session struct {
	ID       string    // short session id (the transcript file's stem)
	Label    string    // the project / cwd it ran in (best effort)
	Tokens   int64     // billed tokens
	Cost     float64   // estimated USD
	Msgs     int       // billed messages (0 when unknown, e.g. Codex)
	Modified time.Time // last activity
}

// StartOfToday returns local midnight — the cutoff for the "today" figure.
func StartOfToday() time.Time {
	n := time.Now()
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, n.Location())
}
