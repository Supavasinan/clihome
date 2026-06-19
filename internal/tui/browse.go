package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"clihome/internal/home"
	"clihome/internal/provider"
	"clihome/internal/tokens"
	"clihome/internal/ui"
)

var homesAccent = lipgloss.NewStyle().Foreground(lipgloss.Color("#f0a888"))

// updateBrowse handles keys on the main cockpit. Per-home actions (sync, restore,
// delete) live behind enter/click; only the global commands stay here.
func (m Model) updateBrowse(key string) (tea.Model, tea.Cmd) {
	if len(m.homes) == 0 {
		switch key {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "n":
			m.startNew()
		}
		return m, nil
	}
	switch key {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "up":
		if m.cursor > 0 {
			m.cursor--
			m.status = ""
			m.recompute()
		}
		return m, m.ensure()
	case "down":
		if m.cursor < len(m.homes)-1 {
			m.cursor++
			m.status = ""
			m.recompute()
		}
		return m, m.ensure()
	case "enter", " ", "right", "l":
		m.openActions()
	case "g":
		if ts, ok := m.toks[m.homes[m.cursor].Name]; ok && ts.u != nil && len(ts.u.Daily) > 0 {
			m.startActivity()
		}
	case "p":
		m.openPrefs()
	case "n":
		m.startNew()
	}
	return m, nil
}

// homeRowAt maps (x,y) to a home row index, or -1. The first home row is at
// screen row 8 (border + title + blank + header + blank). Shared by clickHome and
// hover so the highlight tracks exactly where a click would land.
func (m Model) homeRowAt(x, y int) int {
	row := y - 8
	if x < 0 || x >= m.homesW() || row < 0 || row >= len(m.homes) {
		return -1
	}
	return row
}

// clickHome selects the home row under (x,y) and opens its actions. Returns false
// if no row was hit.
func (m *Model) clickHome(x, y int) bool {
	row := m.homeRowAt(x, y)
	if row < 0 {
		return false
	}
	if row != m.cursor {
		m.cursor, m.status = row, ""
		m.recompute()
	}
	m.openActions()
	return true
}

// browseView is the main cockpit: the Homes table + the focused home's Details,
// with the activity heatmap across the bottom. It also renders the "activity"
// mode (same layout, different footer + an active day cursor).
func (m Model) browseView() string {
	// breadcrumb trail (left) + clickable gear button pinned top-right
	header := m.header()

	var footer string
	switch {
	case m.mode == "actions" && m.confirmDelete:
		footer = "  " + ui.Bold.Render("y") + ui.Dim.Render(" delete · ") + ui.Bold.Render("n") + ui.Dim.Render("/esc cancel")
	case m.mode == "actions":
		footer = "  " + ui.Dim.Render("↑/↓ choose · ") + ui.Bold.Render("enter") + ui.Dim.Render(" do · esc back")
		if m.status != "" {
			footer = "  " + m.status + "\n" + footer
		}
	case m.mode == "activity":
		footer = "  " + ui.Dim.Render("←/→/↑/↓ day · ") + ui.Bold.Render("g") + ui.Dim.Render("/esc back")
	case m.status != "":
		footer = "  " + m.status
	default:
		footer = "  " + ui.Dim.Render("↑/↓ move · ") + ui.Bold.Render("enter") + ui.Dim.Render(" manage · n new · g activity · p prefs · q quit") +
			"   " + ui.Dim.Render(ui.Plural(len(m.homes), "home"))
	}

	innerW := m.innerW()
	leftW := m.homesW()
	rightW := innerW - leftW
	actH := m.actH()
	topH := m.bodyH() - actH

	// The left "homes" pane is replaced by the focused home's manage menu in
	// actions mode (and while exploring a day opened from manage) — the rest of
	// the cockpit (details + activity) stays put.
	leftTitle, leftBody := "homes", m.homesList()
	if m.showManage() {
		leftTitle, leftBody = "manage", m.actionsPanel()
	}
	homesP := ui.Pane(leftTitle, leftBody, leftW, topH-2)
	details := ui.Pane("details", m.detailsBody(), rightW, topH-2)
	body := lipgloss.JoinHorizontal(lipgloss.Top, homesP, "  ", details)
	// The activity strip is dropped on short terminals (actH == 0); the homes and
	// details panes then take the full body height.
	if actH > 0 {
		actP := ui.Pane("activity", m.activityBody(), innerW+4, actH-2)
		body = lipgloss.JoinVertical(lipgloss.Left, body, actP)
	}
	return ui.Screen(header, body, footer)
}

// homesW is the homes pane's outer width (shared by View and the selection bar).
func (m Model) homesW() int { return m.innerW() * 64 / 100 }

func (m Model) homesList() string {
	rows, cw := ui.Rows(m.homes)
	const g = "  "
	// per-row computed cells: plan (live where known), usage windows, tokens, cost
	type cell struct{ plan, sess, week, toks, cost string }
	cs := make([]cell, len(rows))
	wp, ws, ww, wt, wc := cw.Plan, len("SESSION"), len("WEEKLY"), len("TOKENS"), len("COST")
	for i, r := range rows {
		c := cell{plan: r.Plan, sess: m.winPct(r.Name, 0), week: m.winPct(r.Name, 1)}
		if st, ok := m.usage[r.Name]; ok && st.u != nil && st.u.Plan != "" {
			c.plan = st.u.Plan
		}
		c.toks, c.cost = m.totalTokCost(r.Name)
		cs[i] = c
		wp, ws, ww = max(wp, len(c.plan)), max(ws, len(c.sess)), max(ww, len(c.week))
		wt, wc = max(wt, len(c.toks)), max(wc, len(c.cost))
	}

	// Columns in display order. Lower prio is dropped first when the pane is
	// too narrow, so the table stays on one line per row (responsive).
	type col struct {
		head string
		w    int
		prio int
		val  func(i int) string
	}
	cols := []col{
		{"HOME", cw.Name, 100, func(i int) string { return rows[i].Name }},
		{"TOOL", cw.Tool, 50, func(i int) string { return rows[i].Tool }},
		{"ACCOUNT", cw.Account, 40, func(i int) string { return rows[i].Account }},
		{"PLAN", wp, 35, func(i int) string { return cs[i].plan }},
		{"SESSION", ws, 25, func(i int) string { return cs[i].sess }},
		{"WEEKLY", ww, 25, func(i int) string { return cs[i].week }},
		{"TOKENS", wt, 90, func(i int) string { return cs[i].toks }},
		{"COST", wc, 80, func(i int) string { return cs[i].cost }},
	}
	vis := make([]bool, len(cols))
	for i := range vis {
		vis[i] = true
	}
	tableW := func() int {
		w, n := 0, 0
		for i, c := range cols {
			if vis[i] {
				w, n = w+c.w, n+1
			}
		}
		return w + max(n-1, 0)*len(g)
	}
	for budget := m.homesW() - 4; tableW() > budget; {
		lo, loP := -1, 1<<30
		for i, c := range cols {
			if vis[i] && c.prio < loP {
				lo, loP = i, c.prio
			}
		}
		if lo < 0 {
			break
		}
		vis[lo] = false
	}

	// the visible column just before TOKENS carries the "total" label
	labelCol, prev := -1, -1
	for j, c := range cols {
		if !vis[j] {
			continue
		}
		if c.head == "TOKENS" {
			labelCol = prev
			break
		}
		prev = j
	}

	// Render with the shared ui table component (the same one the sessions table
	// uses). The homes-specific bits — responsive column dropping above, the
	// ▌ accent, per-row tint, and the "+ new home" row — stay here.
	var visIdx []int
	for j := range cols {
		if vis[j] {
			visIdx = append(visIdx, j)
		}
	}
	visCols := make([]ui.Col, len(visIdx))
	for k, j := range visIdx {
		visCols[k] = ui.Col{Title: cols[j].head, Width: cols[j].w}
	}
	cellsFor := func(i int) []ui.Cell {
		cells := make([]ui.Cell, len(visIdx))
		for k, j := range visIdx {
			style := ui.Dim
			if cols[j].head == "HOME" {
				style = ui.Tint(rows[i].Codex)
			}
			cells[k] = ui.Cell{Text: cols[j].val(i), Style: style}
		}
		return cells
	}

	var b strings.Builder
	b.WriteString("  " + ui.TableHeader(visCols) + "\n\n")

	barW := max(m.homesW()-3, 10)
	for i := range rows {
		switch {
		case i == m.cursor:
			b.WriteString(homesAccent.Render("▌") + ui.TableRowBar(visCols, cellsFor(i), barW) + "\n")
		case i == m.homeHover:
			b.WriteString(ui.HoverGutter() + ui.TableRow(visCols, cellsFor(i)) + "\n")
		default:
			b.WriteString("  " + ui.TableRow(visCols, cellsFor(i)) + "\n")
		}
	}

	// "+ new home" row (click it or press n) — turns white on hover
	if m.hoverNew {
		b.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Bold(true).Render("+ new home") + "\n")
	} else {
		b.WriteString("  " + ui.Clay.Render("+") + ui.Dim.Render(" new home") + "\n")
	}

	// grand-total row across all homes (tokens + cost)
	var sumTok int64
	var sumCost float64
	have := false
	for _, r := range rows {
		if st, ok := m.toks[r.Name]; ok && st.u != nil {
			sumTok += st.u.Total.Tokens
			sumCost += st.u.Total.Cost
			have = true
		}
	}
	if have {
		totals := make([]ui.Cell, len(visIdx))
		for k, j := range visIdx {
			switch cols[j].head {
			case "TOKENS":
				totals[k] = ui.Cell{Text: ui.FmtTokens(sumTok), Style: ui.Cream.Bold(true)}
			case "COST":
				totals[k] = ui.Cell{Text: ui.FmtCost(sumCost), Style: ui.Green.Bold(true)}
			default:
				if j == labelCol {
					totals[k] = ui.Cell{Text: ui.RightAlign("total", cols[j].w), Style: ui.Dim}
				}
			}
		}
		b.WriteString("\n  " + ui.TableRow(visCols, totals) + "\n")
	}
	return b.String()
}

func (m Model) detailsBody() string {
	h := m.homes[m.cursor]
	var b strings.Builder
	row := func(k, v string) { b.WriteString(ui.Dim.Render(ui.Pad(k, 12)) + " " + v + "\n") }
	row("home", ui.Dim.Render(h.Dir))
	row("state", stateBadge(m.states[m.cursor]))
	for _, kv := range h.Provider.InfoRows(h.Dir) {
		v := kv[1]
		if kv[0] == "model" || kv[0] == "plan" {
			v = ui.Cream.Render(v)
		}
		row(kv[0], v)
	}
	row("last sync", ui.RelTime(home.LastActive(h)))
	return b.String()
}

// usageBody renders the focused home's live rate-limit windows (network), local
// token/cost aggregates, and account + plan — each section degrades on its own.
func (m Model) usageBody(w int) string {
	h := m.homes[m.cursor]
	var b strings.Builder

	st := m.usage[h.Name]
	switch {
	case st.loading || (st == usageState{}):
		b.WriteString(ui.Dim.Render("fetching usage…") + "\n")
	case st.err != nil:
		b.WriteString(ui.Dim.Render("usage unavailable — "+st.err.Error()) + "\n")
	case st.u == nil || len(st.u.Windows) == 0:
		b.WriteString(ui.Dim.Render("no usage data") + "\n")
	default:
		barW := max(min(w-44, 22), 6)
		for _, win := range st.u.Windows {
			b.WriteString(usageRow(win, barW) + "\n")
		}
	}

	// token + cost aggregates from local transcripts
	if ts, ok := m.toks[h.Name]; ok {
		b.WriteString("\n")
		switch {
		case ts.loading:
			b.WriteString(ui.Dim.Render("counting tokens…") + "\n")
		case ts.err == nil && ts.u != nil:
			b.WriteString(tokenRow("total", ts.u.Total) + "\n")
		}
	}

	return b.String()
}

// tokenRow renders "label   12.3M tok  ·  ~$4.56".
func tokenRow(label string, s tokens.Stat) string {
	return ui.Dim.Render(ui.Pad(label, 8)) + " " + ui.Cream.Render(ui.FmtTokens(s.Tokens)) +
		ui.Dim.Render(" tok  ·  ") + ui.Green.Render(ui.FmtCost(s.Cost))
}

// usageRow renders one window as: label · NN% left · bar · reset time.
func usageRow(win provider.UsageWindow, barW int) string {
	left := win.UsedPercent
	left = 100 - left
	if left < 0 {
		left = 0
	} else if left > 100 {
		left = 100
	}
	col := ui.LevelColor(left)
	return ui.Dim.Render(ui.Pad(win.Label, 8)) + " " +
		col.Render(fmt.Sprintf("%3.0f%% left", left)) + "  " + ui.Gauge(left, barW) + "  " + ui.Dim.Render(resetLabel(win.ResetAt))
}

// resetLabel humanizes a window reset time, compactly ("↻3h10m" / "↻Mon 3pm").
func resetLabel(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Until(t)
	switch {
	case d <= 0:
		return "↻now"
	case d < time.Hour:
		return fmt.Sprintf("↻%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("↻%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return "↻" + t.Format("Mon 3pm")
}

// winPct returns "NN%" remaining for usage window idx, or a placeholder.
func (m Model) winPct(name string, idx int) string {
	st, ok := m.usage[name]
	switch {
	case !ok || st.loading:
		return "…"
	case st.err != nil || st.u == nil || idx >= len(st.u.Windows):
		return "—"
	}
	left := max(100-st.u.Windows[idx].UsedPercent, 0)
	return fmt.Sprintf("%.0f%%", left)
}

// winLeft returns the percent of usage window idx still available, or (0,false).
func (m Model) winLeft(name string, idx int) (float64, bool) {
	st, ok := m.usage[name]
	if !ok || st.loading || st.err != nil || st.u == nil || idx >= len(st.u.Windows) {
		return 0, false
	}
	return max(100-st.u.Windows[idx].UsedPercent, 0), true
}

// usageBar renders "NN% ███░░ ↻3h10m" — the percent left, a 6-cell gauge tinted
// by level (green plenty / amber low / red almost out), and the window's reset
// time (omitted when the provider doesn't report one).
func (m Model) usageBar(name string, idx int) string {
	v, ok := m.winLeft(name, idx)
	if !ok {
		if st, done := m.usage[name]; !done || st.loading {
			return ui.Dim.Render("…")
		}
		return ui.Dim.Render("—")
	}
	const n = 6
	out := ui.LevelColor(v).Render(fmt.Sprintf("%.0f%%", v)) + " " + ui.Gauge(v, n)
	if st := m.usage[name]; st.u != nil && idx < len(st.u.Windows) {
		if r := resetLabel(st.u.Windows[idx].ResetAt); r != "" {
			out += "  " + ui.Dim.Render(r)
		}
	}
	return out
}

// totalTokCost returns the all-time tokens + cost for a home, or placeholders.
func (m Model) totalTokCost(name string) (string, string) {
	st, ok := m.toks[name]
	switch {
	case !ok || st.loading:
		return "…", "…"
	case st.err != nil || st.u == nil:
		return "—", "—"
	}
	return ui.FmtTokens(st.u.Total.Tokens), ui.FmtCost(st.u.Total.Cost)
}
