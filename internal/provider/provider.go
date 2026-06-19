// Package provider describes each AI CLI whose config homes clihome manages.
//
// Adding a tool — for contributors:
//
//	Create one file in this package (e.g. gemini.go) with an init() that calls
//	Register() with a *Provider. Everything else — discovery, the homes table,
//	sync, aliases, new, delete, info — is generic and picks it up automatically.
//
//	func init() {
//	    Register(&Provider{
//	        ID: "gemini", Label: "Gemini", Prefix: "gemini",
//	        Env: "GEMINI_CONFIG_DIR", Command: "gemini",
//	        Manifest:  []string{"settings.json", "commands"},
//	        DenyPaths: []string{"oauth_creds.json", "sessions", "cache"},
//	        DenyRegex: []string{`\.(log|lock)$`},
//	        // optional: ActiveFiles, EmailFn, ModelFn, InfoRowsFn
//	    })
//	}
package provider

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"clihome/internal/tokens"
)

// Provider is the declarative description of one AI CLI.
type Provider struct {
	ID      string // unique id, e.g. "gemini"
	Label   string // display name shown in the TOOL column
	Prefix  string // home-dir prefix (no dot): ~/.<prefix>, ~/.<prefix>2
	Env     string // env var that points the CLI at a home
	Command string // the CLI binary

	ActiveFiles []string // newest mtime = "last active"
	Manifest    []string // portable-config paths/globs synced by default
	DenyPaths   []string // exact/prefix paths never synced (auth/state/caches)
	DenyRegex   []string // regex paths never synced

	// Optional readers — accessed via the nil-safe methods below.
	EmailFn    func(dir string) string
	ModelFn    func(dir string) string
	PlanFn     func(dir string) string
	InfoRowsFn func(dir string) [][2]string
	UsageFn    func(dir string) (*Usage, error)
	TokensFn   func(dir string) (*tokens.Usage, error)
	SessionsFn func(dir string) ([]tokens.Session, error)

	denyRE []*regexp.Regexp
}

// UsageWindow is one subscription rate-limit window (e.g. the 5-hour session).
type UsageWindow struct {
	Label       string    // "Session" | "Weekly" | "Opus"
	UsedPercent float64   // 0..100 consumed
	ResetAt     time.Time // when the window resets (zero if unknown)
}

// Usage is a home's live subscription usage, fetched from the provider's API.
type Usage struct {
	Account string
	Plan    string
	Windows []UsageWindow
}

func (p *Provider) compile() {
	for _, s := range p.DenyRegex {
		p.denyRE = append(p.denyRE, regexp.MustCompile(s))
	}
}

// Deny reports whether a home-relative path must never be synced.
func (p *Provider) Deny(rel string) bool {
	for _, d := range p.DenyPaths {
		if rel == d || strings.HasPrefix(rel, d+"/") {
			return true
		}
	}
	for _, re := range p.denyRE {
		if re.MatchString(rel) {
			return true
		}
	}
	return false
}

// Email returns the account email/identity for a home, or "".
func (p *Provider) Email(dir string) string {
	if p.EmailFn != nil {
		return p.EmailFn(dir)
	}
	return ""
}

// Model returns the configured model for a home, or "".
func (p *Provider) Model(dir string) string {
	if p.ModelFn != nil {
		return p.ModelFn(dir)
	}
	return ""
}

// Plan returns the subscription/plan label for a home, or "".
func (p *Provider) Plan(dir string) string {
	if p.PlanFn != nil {
		return p.PlanFn(dir)
	}
	return ""
}

// InfoRows returns label/value rows shown on the `info` screen.
func (p *Provider) InfoRows(dir string) [][2]string {
	if p.InfoRowsFn != nil {
		return p.InfoRowsFn(dir)
	}
	return nil
}

// Usage fetches live subscription usage for a home, or (nil, nil) if the
// provider doesn't support it. Results are cached on disk (short TTL) and the
// last-known value is served on error, so rapid relaunches don't hit rate limits.
func (p *Provider) Usage(dir string) (*Usage, error) {
	if p.UsageFn == nil {
		return nil, nil
	}
	return cachedUsage(p.ID+"-"+filepath.Base(dir), func() (*Usage, error) { return p.UsageFn(dir) })
}

// Tokens aggregates token usage + estimated cost from a home's transcripts, or
// (nil, nil) if unsupported. Reads local files only (no network).
func (p *Provider) Tokens(dir string) (*tokens.Usage, error) {
	if p.TokensFn != nil {
		return p.TokensFn(dir)
	}
	return nil, nil
}

// Sessions lists a home's individual sessions with per-session tokens + cost
// (most recent first), or (nil, nil) if unsupported. Reads local files only.
func (p *Provider) Sessions(dir string) ([]tokens.Session, error) {
	if p.SessionsFn != nil {
		return p.SessionsFn(dir)
	}
	return nil, nil
}

// ── registry ─────────────────────────────────────────────────────────────────

var registry []*Provider

// Register adds a provider. Call it from a provider file's init().
func Register(p *Provider) {
	p.compile()
	registry = append(registry, p)
}

// All returns every registered provider, in registration (filename) order.
func All() []*Provider { return registry }

// ByID returns the provider with the given id, or nil.
func ByID(id string) *Provider {
	for _, p := range registry {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// ── shared helpers for provider files ────────────────────────────────────────

func readJSON(file string) map[string]any {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	return m
}

func str(m map[string]any, key string) string {
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}

// tomlValue grabs a top-level `key = "value"` from a .toml file (no full parse).
func tomlValue(file, key string) string {
	b, err := os.ReadFile(file)
	if err != nil {
		return ""
	}
	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(key) + `\s*=\s*"?([^"\n]+)"?`)
	if m := re.FindSubmatch(b); m != nil {
		return strings.TrimSpace(string(m[1]))
	}
	return ""
}

// httpGetJSON performs a GET with the given headers and decodes a JSON body
// into out. Non-200 responses return an error carrying the status code.
func httpGetJSON(url string, headers map[string]string, out any) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := (&http.Client{Timeout: 8 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// jwtPayload decodes the (unverified) claims of a JWT id_token (best effort).
func jwtPayload(idToken string) map[string]any {
	parts := strings.Split(idToken, ".")
	if len(parts) < 2 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var j map[string]any
	if json.Unmarshal(payload, &j) != nil {
		return nil
	}
	return j
}

// jwtEmail decodes the email from a JWT id_token payload (best effort).
func jwtEmail(idToken string) string {
	j := jwtPayload(idToken)
	if j == nil {
		return ""
	}
	if e := str(j, "email"); e != "" {
		return e
	}
	for _, k := range []string{"https://api.openai.com/auth", "https://api.openai.com/profile"} {
		if prof, ok := j[k].(map[string]any); ok {
			if e := str(prof, "email"); e != "" {
				return e
			}
		}
	}
	return ""
}

// jwtAccountID pulls the ChatGPT account id from a Codex id_token (best effort).
func jwtAccountID(idToken string) string {
	j := jwtPayload(idToken)
	if j == nil {
		return ""
	}
	if a, ok := j["https://api.openai.com/auth"].(map[string]any); ok {
		if id := str(a, "chatgpt_account_id"); id != "" {
			return id
		}
	}
	return ""
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// firstNonEmpty returns the first non-empty string, or "".
func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func titleCase(s string) string {
	w := strings.Fields(s)
	for i, x := range w {
		if x != "" {
			w[i] = strings.ToUpper(x[:1]) + x[1:]
		}
	}
	return strings.Join(w, " ")
}
