package provider

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"clihome/internal/tokens"
)

func init() {
	Register(&Provider{
		ID:          "codex",
		Label:       "Codex",
		Prefix:      "codex",
		Env:         "CODEX_HOME",
		Command:     "codex",
		ActiveFiles: []string{"history.jsonl", "config.toml"},
		Manifest:    []string{"config.toml", "rules", "skills", "memories", "prompts", "AGENTS.md"},
		DenyPaths: []string{
			"auth.json", "installation_id", "cache", "history.jsonl", "sessions", "log", "logs",
			"shell_snapshots", "tmp", ".tmp", "generated_images", "models_cache.json",
			"version.json", ".personality_migration",
		},
		DenyRegex:  []string{`\.sqlite(-shm|-wal)?$`, `\.(lock|log)$`, `-?cache\.json$`, `^sessions/`, `^logs?/`},
		EmailFn:    codexEmail,
		ModelFn:    func(dir string) string { return tomlValue(filepath.Join(dir, "config.toml"), "model") },
		InfoRowsFn: codexInfoRows,
		UsageFn:    codexUsage,
		TokensFn:   codexTokens,
		SessionsFn: codexSessions,
	})
}

// codexSessions returns one Session per transcript under sessions/, with its
// cumulative token total + cost and (best effort) the cwd it ran in, sorted
// most-recent first.
func codexSessions(dir string) ([]tokens.Session, error) {
	root := filepath.Join(dir, "sessions")
	if _, err := os.Stat(root); err != nil {
		return nil, nil
	}
	var out []tokens.Session
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".jsonl") {
			return nil
		}
		tot, _ := codexParseFile(p, tokens.StartOfToday())
		if tot.Tokens == 0 {
			return nil
		}
		s := tokens.Session{ID: codexSessionID(p), Label: codexSessionLabel(p), Tokens: tot.Tokens, Cost: tot.Cost}
		if fi, e := d.Info(); e == nil {
			s.Modified = fi.ModTime()
		}
		out = append(out, s)
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Modified.After(out[j].Modified) })
	return out, nil
}

// codexSessionID shortens a "rollout-…-<uuid>.jsonl" file name to the uuid stem.
func codexSessionID(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	if i := strings.LastIndex(base, "-"); i >= 0 && i+1 < len(base) {
		return base[i+1:]
	}
	return base
}

// codexSessionLabel reads the session's cwd from its meta line (best effort).
func codexSessionLabel(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<16)
	for i := 0; i < 40; i++ {
		line, err := r.ReadBytes('\n')
		if bytes.Contains(line, []byte(`"cwd"`)) {
			var rec struct {
				Cwd     string `json:"cwd"`
				Payload struct {
					Cwd string `json:"cwd"`
				} `json:"payload"`
			}
			if json.Unmarshal(line, &rec) == nil {
				if cwd := firstNonEmpty(rec.Cwd, rec.Payload.Cwd); cwd != "" {
					return filepath.Base(cwd)
				}
			}
		}
		if err != nil {
			break
		}
	}
	return ""
}

func codexEmail(dir string) string {
	t, _ := readJSON(filepath.Join(dir, "auth.json"))["tokens"].(map[string]any)
	if t == nil {
		return ""
	}
	if idt := str(t, "id_token"); idt != "" {
		if e := jwtEmail(idt); e != "" {
			return e
		}
	}
	if _, ok := t["access_token"]; ok {
		return "logged in"
	}
	return ""
}

// codexUsage fetches the primary/secondary rate-limit windows from the ChatGPT
// backend, using the home's stored access token + account id.
func codexUsage(dir string) (*Usage, error) {
	t, _ := readJSON(filepath.Join(dir, "auth.json"))["tokens"].(map[string]any)
	token := str(t, "access_token")
	if token == "" {
		return nil, fmt.Errorf("not logged in")
	}
	headers := map[string]string{"Authorization": "Bearer " + token}
	acctID := str(t, "account_id")
	if acctID == "" {
		acctID = jwtAccountID(str(t, "id_token"))
	}
	if acctID != "" {
		headers["ChatGPT-Account-Id"] = acctID
	}
	var r struct {
		Email     string `json:"email"`
		PlanType  string `json:"plan_type"`
		RateLimit struct {
			Primary   *codexWindow `json:"primary_window"`
			Secondary *codexWindow `json:"secondary_window"`
		} `json:"rate_limit"`
	}
	if err := httpGetJSON("https://chatgpt.com/backend-api/wham/usage", headers, &r); err != nil {
		return nil, err
	}
	u := &Usage{Account: orDash(firstNonEmpty(r.Email, codexEmail(dir))), Plan: titleCase(r.PlanType)}
	if r.RateLimit.Primary != nil {
		u.Windows = append(u.Windows, r.RateLimit.Primary.window("Session"))
	}
	if r.RateLimit.Secondary != nil {
		u.Windows = append(u.Windows, r.RateLimit.Secondary.window("Weekly"))
	}
	return u, nil
}

// codexTokens aggregates token usage from a home's session transcripts. Each
// session file is independent (no cross-file dedup); its total is the final
// cumulative total_token_usage.
func codexTokens(dir string) (*tokens.Usage, error) {
	root := filepath.Join(dir, "sessions")
	if _, err := os.Stat(root); err != nil {
		return &tokens.Usage{}, nil
	}
	since := tokens.StartOfToday()
	u := tokens.Usage{Daily: map[string]tokens.Stat{}}
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".jsonl") {
			return nil
		}
		tot, today := codexParseFile(p, since)
		u.Total.Tokens += tot.Tokens
		u.Total.Cost += tot.Cost
		u.Today.Tokens += today.Tokens
		u.Today.Cost += today.Cost
		if fi, e := d.Info(); e == nil && tot.Tokens > 0 {
			day := fi.ModTime().Local().Format("2006-01-02")
			s := u.Daily[day]
			s.Tokens += tot.Tokens
			s.Cost += tot.Cost
			u.Daily[day] = s
		}
		return nil
	})
	return &u, nil
}

// codexParseFile reads one Codex session: total_token_usage is cumulative, so
// the session total is its final value. Today is approximated by file mtime.
func codexParseFile(path string, since time.Time) (total, today tokens.Stat) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	var in, cached, out, reason, tot int64
	model := "gpt-5"
	r := bufio.NewReaderSize(f, 1<<16)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 2 {
			var rec struct {
				Payload struct {
					Model string `json:"model"`
					Info  struct {
						TotalTokenUsage struct {
							In     int64 `json:"input_tokens"`
							Cached int64 `json:"cached_input_tokens"`
							Out    int64 `json:"output_tokens"`
							Reason int64 `json:"reasoning_output_tokens"`
							Total  int64 `json:"total_tokens"`
						} `json:"total_token_usage"`
					} `json:"info"`
				} `json:"payload"`
			}
			if json.Unmarshal(line, &rec) == nil {
				if tu := rec.Payload.Info.TotalTokenUsage; tu.Total > 0 {
					in, cached, out, reason, tot = tu.In, tu.Cached, tu.Out, tu.Reason, tu.Total
				}
				if rec.Payload.Model != "" {
					model = rec.Payload.Model
				}
			}
		}
		if err != nil {
			break
		}
	}
	if tot == 0 {
		return
	}
	p := priceFor(model)
	uncached := in - cached
	if uncached < 0 {
		uncached = 0
	}
	cost := float64(uncached)*p.in + float64(cached)*p.cacheR + float64(out+reason)*p.out
	total = tokens.Stat{Tokens: tot, Cost: cost}
	if fi, e := os.Stat(path); e == nil && !fi.ModTime().Before(since) {
		today = total
	}
	return
}

type codexWindow struct {
	UsedPercent float64 `json:"used_percent"`
	ResetAt     int64   `json:"reset_at"`
}

func (w codexWindow) window(label string) UsageWindow {
	var t time.Time
	if w.ResetAt > 0 {
		t = time.Unix(w.ResetAt, 0)
	}
	return UsageWindow{Label: label, UsedPercent: w.UsedPercent, ResetAt: t}
}

func codexInfoRows(dir string) [][2]string {
	cfg := filepath.Join(dir, "config.toml")
	rows := [][2]string{
		{"account", orDash(codexEmail(dir))},
		{"model", orDash(tomlValue(cfg, "model"))},
	}
	if e := tomlValue(cfg, "model_reasoning_effort"); e != "" {
		rows = append(rows, [2]string{"reasoning", e})
	}
	if p := tomlValue(cfg, "personality"); p != "" {
		rows = append(rows, [2]string{"personality", p})
	}
	return rows
}
