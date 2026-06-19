package provider

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"clihome/internal/tokens"
)

func init() {
	Register(&Provider{
		ID:          "claude",
		Label:       "Claude",
		Prefix:      "claude",
		Env:         "CLAUDE_CONFIG_DIR",
		Command:     "claude",
		ActiveFiles: []string{"history.jsonl", "settings.json", "CLAUDE.md"},
		Manifest: []string{
			"CLAUDE.md", "settings.json", "statusline-command.sh", "skills",
			"plugins/installed_plugins.json", "plugins/known_marketplaces.json",
			"projects/*/memory",
		},
		DenyPaths: []string{
			".claude.json", ".credentials.json", "history.jsonl", "sessions", "shell-snapshots",
			"session-env", "todos", "statsig", "daemon", "ide", "jobs", "tasks", "security",
			"debug", "fluent-data", "cache", "image-cache", "telemetry", "file-history",
			"paste-cache", "downloads", "uploads", "backups",
			"plugins/cache", "plugins/marketplaces", "plugins/repos",
		},
		DenyRegex: []string{
			`^\.claude\.json`, `^daemon`, `(^|/)\.last[-_]`,
			`\.(lock|log|bak|orig|vibe-ads-backup)$`, `-?cache\.json$`, `\.cache$`,
			`^projects/.+\.jsonl$`,
		},
		EmailFn:    func(dir string) string { return str(claudeAccount(dir), "emailAddress") },
		ModelFn:    func(dir string) string { return str(claudeSettings(dir), "model") },
		PlanFn:     func(dir string) string { return claudePlan(claudeAccount(dir)) },
		InfoRowsFn: claudeInfoRows,
		UsageFn:    claudeUsage,
		TokensFn:   claudeTokens,
		SessionsFn: claudeSessions,
	})
}

func claudeAccount(dir string) map[string]any {
	files := []string{filepath.Join(dir, ".claude.json")}
	if filepath.Base(dir) == ".claude" {
		if home, err := os.UserHomeDir(); err == nil {
			files = append(files, filepath.Join(home, ".claude.json"))
		}
	}
	out := map[string]any{}
	for _, f := range files {
		a, _ := readJSON(f)["oauthAccount"].(map[string]any)
		for k, v := range a {
			if _, ok := out[k]; !ok {
				out[k] = v
			}
		}
	}
	return out
}

func claudeSettings(dir string) map[string]any {
	return readJSON(filepath.Join(dir, "settings.json"))
}

// claudePlan turns "claude_max" + a "…_5x" rate tier into "Max 5x".
func claudePlan(a map[string]any) string {
	base := str(a, "organizationType")
	if base == "" {
		base = str(a, "seatTier")
	}
	if base == "" {
		return ""
	}
	base = regexp.MustCompile(`^claude[_-]?`).ReplaceAllString(base, "")
	base = titleCase(strings.TrimSpace(strings.ReplaceAll(base, "_", " ")))
	rt := str(a, "userRateLimitTier")
	if rt == "" {
		rt = str(a, "organizationRateLimitTier")
	}
	if m := regexp.MustCompile(`(\d+)\s*x`).FindStringSubmatch(rt); m != nil {
		return base + " " + m[1] + "x"
	}
	return base
}

// claudeCreds returns a home's claudeAiOauth credentials: from its
// .credentials.json if present, else (on macOS) the Keychain, where Claude Code
// stores the token. The Keychain service is "Claude Code-credentials-<h>" where
// <h> is the first 8 hex of sha256(absolute config-dir path); the bare
// "Claude Code-credentials" is the legacy default-home item.
func claudeCreds(dir string) map[string]any {
	if o, ok := readJSON(filepath.Join(dir, ".credentials.json"))["claudeAiOauth"].(map[string]any); ok {
		return o
	}
	if runtime.GOOS != "darwin" {
		return nil
	}
	abs := dir
	if a, err := filepath.Abs(dir); err == nil {
		abs = a
	}
	sum := sha256.Sum256([]byte(abs))
	for _, svc := range []string{"Claude Code-credentials-" + hex.EncodeToString(sum[:])[:8], "Claude Code-credentials"} {
		out, err := exec.Command("security", "find-generic-password", "-s", svc, "-w").Output()
		if err != nil {
			continue
		}
		var c map[string]any
		if json.Unmarshal(out, &c) == nil {
			if o, ok := c["claudeAiOauth"].(map[string]any); ok && str(o, "accessToken") != "" {
				return o
			}
		}
	}
	return nil
}

// claudeUsage fetches the 5-hour / 7-day rate-limit windows from Anthropic's
// OAuth usage endpoint, using the home's stored access token (file or Keychain).
func claudeUsage(dir string) (*Usage, error) {
	if str(claudeAccount(dir), "emailAddress") == "" {
		return nil, fmt.Errorf("not logged in")
	}
	token := str(claudeCreds(dir), "accessToken")
	if token == "" {
		return nil, fmt.Errorf("no access token")
	}
	var r struct {
		FiveHour     *claudeWindow `json:"five_hour"`
		SevenDay     *claudeWindow `json:"seven_day"`
		SevenDayOpus *claudeWindow `json:"seven_day_opus"`
	}
	err := httpGetJSON("https://api.anthropic.com/api/oauth/usage", map[string]string{
		"Authorization":  "Bearer " + token,
		"Accept":         "application/json",
		"Content-Type":   "application/json",
		"anthropic-beta": "oauth-2025-04-20",
	}, &r)
	if err != nil {
		return nil, err
	}
	u := &Usage{Account: str(claudeAccount(dir), "emailAddress"), Plan: claudePlan(claudeAccount(dir))}
	for _, w := range []struct {
		label string
		w     *claudeWindow
	}{{"Session", r.FiveHour}, {"Weekly", r.SevenDay}, {"Opus", r.SevenDayOpus}} {
		if w.w != nil {
			u.Windows = append(u.Windows, w.w.window(w.label))
		}
	}
	return u, nil
}

var claudeUsageKey = []byte(`"usage"`)

type claudeMsgAgg struct {
	in, out, cr, cw int64
	model           string
	day             string // "2006-01-02" of first occurrence ("" if unknown)
}

type claudeRec struct {
	Timestamp string `json:"timestamp"`
	RequestID string `json:"requestId"`
	Message   struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage struct {
			In  int64 `json:"input_tokens"`
			Out int64 `json:"output_tokens"`
			CW  int64 `json:"cache_creation_input_tokens"`
			CR  int64 `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// claudeTokens aggregates token usage from a home's chat transcripts. Claude
// Code's streaming API and resumed sessions write the same message many times,
// so usage is deduped by messageId:requestId (keeping the per-field max), then
// priced per model — matching ccusage / tokscale.
func claudeTokens(dir string) (*tokens.Usage, error) {
	root := filepath.Join(dir, "projects")
	if _, err := os.Stat(root); err != nil {
		return &tokens.Usage{}, nil
	}
	since := tokens.StartOfToday()
	todayStr := since.Format("2006-01-02")
	seen := map[string]*claudeMsgAgg{}
	anon := 0
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".jsonl") {
			return nil
		}
		f, e := os.Open(p)
		if e != nil {
			return nil
		}
		r := bufio.NewReaderSize(f, 1<<16)
		for {
			line, rerr := r.ReadBytes('\n')
			if len(line) > 2 && bytes.Contains(line, claudeUsageKey) {
				var rec claudeRec
				if json.Unmarshal(line, &rec) == nil && rec.Message.Model != "" {
					us := rec.Message.Usage
					if us.In+us.Out+us.CR+us.CW > 0 {
						key := rec.Message.ID
						switch {
						case key == "":
							anon++
							key = "anon:" + strconv.Itoa(anon)
						case rec.RequestID != "":
							key += ":" + rec.RequestID
						default:
							key = "message:" + key
						}
						if a := seen[key]; a != nil {
							a.in = maxI(a.in, us.In)
							a.out = maxI(a.out, us.Out)
							a.cr = maxI(a.cr, us.CR)
							a.cw = maxI(a.cw, us.CW)
							a.model = rec.Message.Model
						} else {
							day := ""
							if t, e := time.Parse(time.RFC3339, rec.Timestamp); e == nil {
								day = t.Local().Format("2006-01-02")
							}
							seen[key] = &claudeMsgAgg{us.In, us.Out, us.CR, us.CW, rec.Message.Model, day}
						}
					}
				}
			}
			if rerr != nil {
				break
			}
		}
		f.Close()
		return nil
	})
	u := tokens.Usage{Daily: map[string]tokens.Stat{}}
	for _, a := range seen {
		tk := a.in + a.out + a.cr + a.cw
		pr := priceFor(a.model)
		cost := float64(a.in)*pr.in + float64(a.out)*pr.out + float64(a.cw)*pr.cacheW + float64(a.cr)*pr.cacheR
		u.Total.Tokens += tk
		u.Total.Cost += cost
		if a.day != "" {
			s := u.Daily[a.day]
			s.Tokens += tk
			s.Cost += cost
			u.Daily[a.day] = s
		}
		if a.day == todayStr {
			u.Today.Tokens += tk
			u.Today.Cost += cost
		}
	}
	return &u, nil
}

func maxI(a, b int64) int64 {
	if b > a {
		return b
	}
	return a
}

// claudeSessions returns one Session per transcript file under projects/, with
// per-file token/cost (deduped within the file) and the project it ran in,
// sorted most-recent first.
func claudeSessions(dir string) ([]tokens.Session, error) {
	root := filepath.Join(dir, "projects")
	if _, err := os.Stat(root); err != nil {
		return nil, nil
	}
	var out []tokens.Session
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".jsonl") {
			return nil
		}
		if s := claudeSessionFile(p); s.Tokens > 0 {
			out = append(out, s)
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Modified.After(out[j].Modified) })
	return out, nil
}

// claudeSessionFile rolls up one transcript: tokens/cost (deduped by
// messageId:requestId, per-field max — the same rule as claudeTokens, but scoped
// to this single file), the billed-message count, project label, and mtime.
func claudeSessionFile(path string) tokens.Session {
	f, err := os.Open(path)
	if err != nil {
		return tokens.Session{}
	}
	defer f.Close()
	seen := map[string]*claudeMsgAgg{}
	anon := 0
	r := bufio.NewReaderSize(f, 1<<16)
	for {
		line, rerr := r.ReadBytes('\n')
		if len(line) > 2 && bytes.Contains(line, claudeUsageKey) {
			var rec claudeRec
			if json.Unmarshal(line, &rec) == nil && rec.Message.Model != "" {
				us := rec.Message.Usage
				if us.In+us.Out+us.CR+us.CW > 0 {
					key := rec.Message.ID
					switch {
					case key == "":
						anon++
						key = "anon:" + strconv.Itoa(anon)
					case rec.RequestID != "":
						key += ":" + rec.RequestID
					default:
						key = "message:" + key
					}
					if a := seen[key]; a != nil {
						a.in, a.out = maxI(a.in, us.In), maxI(a.out, us.Out)
						a.cr, a.cw = maxI(a.cr, us.CR), maxI(a.cw, us.CW)
						a.model = rec.Message.Model
					} else {
						seen[key] = &claudeMsgAgg{us.In, us.Out, us.CR, us.CW, rec.Message.Model, ""}
					}
				}
			}
		}
		if rerr != nil {
			break
		}
	}
	var s tokens.Session
	for _, a := range seen {
		pr := priceFor(a.model)
		s.Tokens += a.in + a.out + a.cr + a.cw
		s.Cost += float64(a.in)*pr.in + float64(a.out)*pr.out + float64(a.cw)*pr.cacheW + float64(a.cr)*pr.cacheR
	}
	s.Msgs = len(seen)
	s.ID = strings.TrimSuffix(filepath.Base(path), ".jsonl")
	s.Label = claudeProjectLabel(filepath.Base(filepath.Dir(path)))
	if fi, e := os.Stat(path); e == nil {
		s.Modified = fi.ModTime()
	}
	return s
}

// claudeProjectLabel turns Claude's cwd-encoded project dir ("-Users-me-code-foo")
// into a readable tail ("foo").
func claudeProjectLabel(dirName string) string {
	parts := strings.Split(strings.Trim(dirName, "-"), "-")
	if n := len(parts); n > 0 && parts[n-1] != "" {
		return parts[n-1]
	}
	return dirName
}

type claudeWindow struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    string  `json:"resets_at"`
}

func (w claudeWindow) window(label string) UsageWindow {
	t, _ := time.Parse(time.RFC3339, w.ResetsAt)
	return UsageWindow{Label: label, UsedPercent: w.Utilization, ResetAt: t}
}

func claudeInfoRows(dir string) [][2]string {
	a := claudeAccount(dir)
	s := claudeSettings(dir)
	var rows [][2]string
	if v := str(a, "displayName"); v != "" {
		rows = append(rows, [2]string{"name", v})
	}
	if org := str(a, "organizationName"); org != "" {
		v := org
		if ot := str(a, "organizationType"); ot != "" {
			v += "  (" + ot + ")"
		}
		rows = append(rows, [2]string{"organization", v})
	}
	if plan := claudePlan(a); plan != "" {
		rows = append(rows, [2]string{"plan", plan})
	}
	if v := str(a, "billingType"); v != "" {
		rows = append(rows, [2]string{"billing", v})
	}
	plugins := 0
	if ep, ok := s["enabledPlugins"].(map[string]any); ok {
		plugins = len(ep)
	}
	rows = append(rows,
		[2]string{"model", orDash(str(s, "model"))},
		[2]string{"theme", orDash(str(s, "theme"))},
		[2]string{"plugins", strconv.Itoa(plugins)},
	)
	return rows
}
