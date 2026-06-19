// Package tui implements clihome's interactive cockpit with Bubble Tea: a Homes
// list, the selected home's Details, and a daily-activity heatmap. Clicking (or
// pressing enter on) a home opens its actions — Sync, Restore, or Delete.
//
// The screen is one Bubble Tea Model whose state is split across files, one per
// screen: browse.go, actions.go, sync.go, restore.go, activity.go, prefs.go and
// newhome.go. Shared sync-state lives in state.go; small cross-screen helpers in
// helpers.go; reusable rendering primitives in the internal/ui package.
package tui

import (
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"clihome/internal/home"
	"clihome/internal/plan"
	"clihome/internal/provider"
	"clihome/internal/syncer"
	"clihome/internal/tokens"
	"clihome/internal/ui"
)

// Model holds every screen's state; m.mode selects which screen renders.
type Model struct {
	homes  []home.Home
	cursor int
	w, h   int

	states []string   // per-home sync state: base/clean/drift/—
	fp     plan.Plan  // diff for the current source → target
	fpSrc  *home.Home // sync source
	fpDst  *home.Home // sync target
	fpKind string     // "diff" | "clean" | "synced-base" | "single"

	mode   string   // "browse" | "actions" | "sync" | "restore" | "activity" | "prefs" | "new"
	nav    []string // screen back-stack: esc pops one level instead of jumping home
	status string   // transient result line in the footer

	prefs      prefs // user preferences (heatmap palette, activity range)
	prefCur    int   // selected preference field
	newCur     int   // selected provider in the new-home picker
	hoverNew   bool  // mouse is over the "+ new home" row
	crumbHover int   // breadcrumb segment under the mouse (-1 = none)

	// live hover targets, recomputed on every mouse event (-1/false = none). These
	// drive the cream "hover" affordance on each clickable element.
	homeHover      int  // home row under the mouse (browse)
	gearHover      bool // ⚙ prefs button under the mouse
	actionHover    int  // manage-menu action row under the mouse
	heatHoverC     int  // heatmap day column under the mouse
	heatHoverR     int  // heatmap day row under the mouse
	newHover       int  // provider-picker row under the mouse
	prefHoverField int  // preferences row under the mouse
	prefHoverOpt   int  // preferences option chip under the mouse

	// home-actions menu (mode "actions")
	actionCur     int  // highlighted action row
	confirmDelete bool // delete confirmation is showing

	// activity heatmap day cursor (mode "activity")
	actRow, actCol int

	// the active apply process (sync or restore)
	procTitle   string // "sync" | "restore"
	procSrc     string // source label shown in the header
	procDst     string // target label shown in the header
	procDstName string // target home name (for naming the new restore point)
	syncEntries []plan.Entry
	syncSel     []bool
	syncCursor  int
	diffScroll  int

	// restore state
	restorePts     []syncer.RestorePoint
	restoreCur     int // highlighted restore point (left pane)
	restoreFileCur int // highlighted file within that point (middle pane)

	// live usage + token aggregates, fetched async per home
	usage map[string]usageState
	toks  map[string]tokState

	// session history (mode "history"), fetched async per home
	sessions   map[string]sessState
	sessCursor int
	sessScroll int
	sess       ui.Sort // session table sort state (column/direction/hover)
}

// usageState is the per-home result of a usage fetch.
type usageState struct {
	loading bool
	u       *provider.Usage
	err     error
}

// usageMsg delivers a completed usage fetch back to Update.
type usageMsg struct {
	home string
	u    *provider.Usage
	err  error
}

// tokState is the per-home result of a token-aggregation pass.
type tokState struct {
	loading bool
	u       *tokens.Usage
	err     error
}

// tokMsg delivers a completed token aggregation back to Update.
type tokMsg struct {
	home string
	u    *tokens.Usage
	err  error
}

// New builds the cockpit, precomputing each home's sync state.
func New() Model {
	m := Model{homes: home.Discover(), mode: "browse", usage: map[string]usageState{}, toks: map[string]tokState{}, sessions: map[string]sessState{}}
	m.crumbHover = -1
	m.clearHover()
	m.sess = ui.Sort{Col: 5, Asc: false, Hover: -1} // newest sessions first
	m.prefs = loadPrefs()
	applyPrefs(m.prefs)
	m.states = make([]string, len(m.homes))
	for i := range m.homes {
		m.states[i] = m.stateOf(i)
	}
	if len(m.homes) > 0 {
		m.recompute()
	}
	return m
}

func (m Model) Init() tea.Cmd { return nil }

// ── async fetches ─────────────────────────────────────────────────────────────

// fetchUsage runs the focused home's usage request off the UI thread.
func fetchUsage(h home.Home) tea.Cmd {
	return func() tea.Msg {
		u, err := h.Provider.Usage(h.Dir)
		return usageMsg{home: h.Name, u: u, err: err}
	}
}

// fetchTokens aggregates the focused home's transcripts off the UI thread.
func fetchTokens(h home.Home) tea.Cmd {
	return func() tea.Msg {
		u, err := h.Provider.Tokens(h.Dir)
		return tokMsg{home: h.Name, u: u, err: err}
	}
}

// ensure kicks off the usage + token fetches for the focused home if not yet done.
func (m *Model) ensure() tea.Cmd {
	if len(m.homes) == 0 {
		return nil
	}
	return m.fetchFor(m.homes[m.cursor])
}

// ensureAll fetches usage + tokens for every home (the homes table shows them
// all as columns).
func (m *Model) ensureAll() tea.Cmd {
	var cmds []tea.Cmd
	for _, h := range m.homes {
		if c := m.fetchFor(h); c != nil {
			cmds = append(cmds, c)
		}
	}
	return tea.Batch(cmds...)
}

func (m *Model) fetchFor(h home.Home) tea.Cmd {
	var cmds []tea.Cmd
	if _, done := m.usage[h.Name]; !done {
		m.usage[h.Name] = usageState{loading: true}
		cmds = append(cmds, fetchUsage(h))
	}
	if _, done := m.toks[h.Name]; !done {
		m.toks[h.Name] = tokState{loading: true}
		cmds = append(cmds, fetchTokens(h))
	}
	return tea.Batch(cmds...)
}

// ── top-level update / view routing ───────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		return m, m.ensureAll()
	case usageMsg:
		m.usage[msg.home] = usageState{u: msg.u, err: msg.err}
		return m, nil
	case tokMsg:
		m.toks[msg.home] = tokState{u: msg.u, err: msg.err}
		return m, nil
	case sessMsg:
		m.sessions[msg.home] = sessState{s: msg.s, err: msg.err}
		return m, nil
	case tea.MouseMsg:
		// Recompute every screen's hover targets from the mouse position so the
		// cream hover affordance tracks the cursor live (motion events included).
		m.recomputeHover(msg.X, msg.Y)
		// Act on the press only. With WithMouseAllMotion a single click also
		// emits a release (and motions) at the same spot; handling those too
		// would cascade — e.g. press opens a home's actions, then the release
		// lands on the now-visible first action and fires it.
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if msg.Y == 1 {
				if i := m.crumbAt(msg.X); i >= 0 { // jump up the breadcrumb trail
					m.navTo(m.crumbs()[i].depth)
					return m, m.ensure()
				}
			}
			return m, m.handleClick(msg.X, msg.Y)
		}
		return m, nil
	case tea.KeyMsg:
		switch m.mode {
		case "actions":
			return m.updateActions(msg.String())
		case "sync":
			return m.updateSync(msg.String())
		case "restore":
			return m.updateRestore(msg.String())
		case "activity":
			return m.updateActivity(msg.String())
		case "prefs":
			return m.updatePrefs(msg.String())
		case "new":
			return m.updateNew(msg.String())
		case "history":
			return m.updateHistory(msg.String())
		}
		return m.updateBrowse(msg.String())
	}
	return m, nil
}

func (m Model) View() string {
	if m.w == 0 {
		return "\n  loading…\n"
	}
	if len(m.homes) == 0 {
		return ui.Banner("") + "\n   " + ui.Dim.Render("no AI CLI homes found — press n to create one, q to quit") + "\n"
	}
	// Clamp to the terminal height so the frame's top never scrolls off the alt
	// screen — that shift is what makes hover/clicks miss when the window is short.
	return ui.FitHeight(m.screen(), m.h)
}

// screen renders the active mode's full-screen frame (before height clamping).
func (m Model) screen() string {
	switch m.mode {
	case "sync":
		return m.syncView()
	case "restore":
		return m.restoreView()
	case "prefs":
		return m.prefsView()
	case "new":
		return m.newView()
	case "history":
		return m.historyView()
	}
	return m.browseView()
}

// handleClick routes a left-click to whatever sits under the cursor: the ⚙
// button, a home row, a heatmap day, a preference option, or a picker row.
func (m *Model) handleClick(x, y int) tea.Cmd {
	switch m.mode {
	case "prefs":
		m.clickPrefs(x, y)
		return nil
	case "new":
		m.clickNew(x, y)
		return m.ensure()
	case "history":
		m.clickHistory(x, y)
		return nil
	}
	// cockpit modes (browse / actions / activity)
	if len(m.homes) == 0 {
		return nil
	}
	if m.showManage() {
		// left pane is the manage menu; the bottom activity strip stays clickable
		if x >= 0 && x < m.homesW() && y >= manageRowsTop && y < manageRowsTop+len(actionItems) {
			return m.clickActions(x, y)
		}
		m.clickHeat(x, y)
		return nil
	}
	// left pane is the homes table
	if y == 1 && x >= m.w-9 { // ⚙ button, top-right of the header
		m.openPrefs()
		return nil
	}
	if m.clickHome(x, y) {
		return m.ensure()
	}
	if y == 8+len(m.homes) && x >= 0 && x < m.homesW() { // the "+ new home" row
		m.startNew()
		return nil
	}
	m.clickHeat(x, y)
	return nil
}

// ── shared layout geometry ────────────────────────────────────────────────────

func (m Model) innerW() int { return max(m.w-7, 40) }
func (m Model) bodyH() int  { return max(m.h-6, 8) }

// ── screen navigation stack ───────────────────────────────────────────────────

// push opens a screen, remembering the current one so esc can return to it. It
// is cycle-safe: re-entering a screen already in the trail pops back to it
// rather than stacking a duplicate (cockpit screens like actions↔activity can
// otherwise bounce and grow the breadcrumb without bound).
func (m *Model) push(mode string) {
	if m.mode == mode {
		return
	}
	if i := slices.Index(m.nav, mode); i >= 0 {
		m.nav = m.nav[:i]
		m.mode = mode
		return
	}
	m.nav = append(m.nav, m.mode)
	m.mode = mode
}

// showManage reports whether the left cockpit pane shows the manage menu instead
// of the homes table — in actions mode, or while exploring a day opened from
// manage (so the manage context stays put and there's no homes row to re-click).
func (m Model) showManage() bool {
	return m.mode == "actions" || (m.mode == "activity" && slices.Contains(m.nav, "actions"))
}

// goBack returns to the screen that opened the current one (esc/cancel). It pops
// one level off the stack, falling back to the home screen when empty.
func (m *Model) goBack() {
	if n := len(m.nav); n > 0 {
		m.mode = m.nav[n-1]
		m.nav = m.nav[:n-1]
	} else {
		m.mode = "browse"
	}
}

// goHome clears the stack and returns to the cockpit — used after an operation
// completes (sync applied, home created/deleted) rather than on cancel.
func (m *Model) goHome() {
	m.nav = m.nav[:0]
	m.mode = "browse"
}

// crumbText is a screen's breadcrumb label. The per-home actions hub shows the
// focused home's name so the trail reads "clihome › claude2 › restore".
func crumbText(mode, homeName string) string {
	switch mode {
	case "browse":
		return "clihome"
	case "actions":
		if homeName != "" {
			return homeName
		}
		return "actions"
	case "sync":
		return "sync"
	case "restore":
		return "restore"
	case "activity":
		return "" // day exploration is in-page, not a separate screen — no crumb
	case "history":
		return "sessions"
	case "prefs":
		return "preferences"
	case "new":
		return "new home"
	}
	return mode
}

// crumbPrefixW is the width of the "◆ " logo that precedes the trail.
const crumbPrefixW = 2

// crumb is one rendered breadcrumb segment: its label + the nav depth a click
// returns to (in-page modes like activity are skipped, so the rendered index is
// not the nav depth).
type crumb struct {
	text  string
	depth int
}

// crumbs builds the rendered breadcrumb trail from the back-stack + current
// screen, dropping screens with no label (e.g. activity, which is in-page).
func (m Model) crumbs() []crumb {
	homeName := ""
	if len(m.homes) > 0 {
		homeName = m.homes[m.cursor].Name
	}
	modes := append(append([]string{}, m.nav...), m.mode)
	var out []crumb
	for i, mode := range modes {
		if t := crumbText(mode, homeName); t != "" {
			out = append(out, crumb{t, i})
		}
	}
	return out
}

// crumbAt maps an x on the top row to a clickable breadcrumb index, or -1. The
// last crumb is where you already are, so it is not clickable.
func (m Model) crumbAt(x int) int {
	cs := m.crumbs()
	if len(cs) <= 1 {
		return -1
	}
	pos := crumbPrefixW
	for i, c := range cs {
		if i == len(cs)-1 {
			break
		}
		if w := lipgloss.Width(c.text); x >= pos && x < pos+w {
			return i
		} else {
			pos += w + 3 // label + " › "
		}
	}
	return -1
}

// navTo truncates the back-stack to depth i and makes that screen current —
// the click target of a breadcrumb segment.
func (m *Model) navTo(i int) {
	modes := append(append([]string{}, m.nav...), m.mode)
	if i < 0 || i >= len(modes) {
		return
	}
	m.mode = modes[i]
	m.nav = append([]string{}, modes[:i]...)
	m.status = ""
}

// breadcrumb renders the navigation trail, e.g. "◆ clihome › claude2 › restore".
// The root is clay, ancestors dim, the current screen cream, and the segment
// under the mouse brightens to cream to signal it's clickable.
func (m Model) breadcrumb() string {
	cs := m.crumbs()
	sep := ui.Dim.Render(" › ")
	var b strings.Builder
	b.WriteString(ui.Clay.Render("◆") + " ")
	for i, c := range cs {
		if i > 0 {
			b.WriteString(sep)
		}
		switch {
		case i == m.crumbHover && i != len(cs)-1:
			b.WriteString(ui.Cream.Render(c.text)) // hovered + clickable
		case i == 0:
			b.WriteString(ui.Clay.Bold(true).Render(c.text))
		case i == len(cs)-1:
			b.WriteString(ui.Cream.Bold(true).Render(c.text))
		default:
			b.WriteString(ui.Dim.Render(c.text))
		}
	}
	return b.String()
}

// header is the top line shown on every screen: the breadcrumb (left), a short
// context note or the ⚙ button on the cockpit (right).
func (m Model) header() string {
	crumb := m.breadcrumb()
	switch m.mode {
	case "browse":
		crumb += "  " + ui.Dim.Render("·  AI CLI config home manager")
	case "sync":
		crumb += "   " + ui.Dim.Render(m.procSrc+" → "+m.procDst)
	}
	if (m.mode == "browse" || m.mode == "activity") && !m.showManage() {
		gearStyle := ui.Dim
		if m.gearHover {
			gearStyle = ui.Cream
		}
		gear := gearStyle.Render("⚙ prefs")
		pad := max(m.w-lipgloss.Width(crumb)-lipgloss.Width(gear)-2, 1)
		crumb += strings.Repeat(" ", pad) + gear
	}
	// Force exactly one line of width m.w: a header that wraps would push every row
	// down by a line and throw off the mouse hit-tests below it.
	return ui.AnsiPad(crumb, m.w)
}
