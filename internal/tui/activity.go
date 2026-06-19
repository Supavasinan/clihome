package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"clihome/internal/tokens"
	"clihome/internal/ui"
)

// ── activity heatmap geometry (shared by layout, render + navigation) ─────────

const (
	heatLabelW = 4 // width of the "Mon "/"Wed " day labels
	heatCellW  = 2 // each day is a block + a gap, GitHub-style
)

// actH is the height of the full-width activity strip at the bottom, or 0 when the
// terminal is too short to fit it. It leaves at least 11 rows for the homes/details
// panes above and caps at 15 (enough for the grid, legend and selected-day line);
// below ~12 rows of its own there's no usable heatmap, so it hides instead — the
// panes and footer take the space. Because Pane no longer expands, this height is
// exactly what renders, so the heatmap hit-test stays aligned at every size.
func (m Model) actH() int {
	if h := min(m.bodyH()-11, 15); h >= 12 {
		return h
	}
	return 0
}

// actContentW is the activity pane's inner text width (full width minus chrome).
func (m Model) actContentW() int { return m.innerW() + 2 }

// gridW is the width the heatmap gets — the whole activity pane.
func (m Model) gridW() int { return m.actContentW() }

// heatWeeks is how many weeks fit a content width at the current cell width,
// capped by the preferred activity range.
func heatWeeks(contentW int) int {
	return max(min((contentW-heatLabelW)/heatCellW, heatWeekCap), 8)
}

// heatStart returns the Monday of the oldest visible week.
func heatStart(weeks int) time.Time {
	today := tokens.StartOfToday()
	thisMon := today.AddDate(0, 0, -((int(today.Weekday()) + 6) % 7))
	return thisMon.AddDate(0, 0, -7*(weeks-1))
}

func heatDate(start time.Time, col, row int) time.Time { return start.AddDate(0, 0, col*7+row) }

// startActivity enters day-navigation on the heatmap, cursor on today.
func (m *Model) startActivity() {
	today := tokens.StartOfToday()
	m.actCol = heatWeeks(m.gridW()) - 1
	m.actRow = (int(today.Weekday()) + 6) % 7
	m.push("activity")
}

// updateActivity moves the day cursor across the grid.
func (m Model) updateActivity(key string) (tea.Model, tea.Cmd) {
	weeks := heatWeeks(m.gridW())
	start := heatStart(weeks)
	today := tokens.StartOfToday()
	move := func(dc, dr int) {
		nc, nr := m.actCol+dc, m.actRow+dr
		if nc < 0 || nc >= weeks || nr < 0 || nr > 6 || heatDate(start, nc, nr).After(today) {
			return
		}
		m.actCol, m.actRow = nc, nr
	}
	switch key {
	case "left", "h":
		move(-1, 0)
	case "right", "l":
		move(1, 0)
	case "up", "k":
		move(0, -1)
	case "down", "j":
		move(0, 1)
	case "esc", "q", "ctrl+c", "g":
		m.goBack()
	}
	return m, nil
}

// heatCellAt maps (x,y) to a heatmap day (col,row), or ok=false. The activity
// strip spans the bottom: day row r is at screen y = (bodyH-actH)+7+r, a cell at
// x = 6+col*cellW. Future days and empty/loading grids never hit. Shared by
// clickHeat and hover so the highlight tracks exactly where a click would land.
func (m Model) heatCellAt(x, y int) (int, int, bool) {
	if m.actH() == 0 { // activity strip hidden on short terminals
		return 0, 0, false
	}
	ts, ok := m.toks[m.homes[m.cursor].Name]
	if !ok || ts.u == nil || len(ts.u.Daily) == 0 {
		return 0, 0, false
	}
	row := y - (m.bodyH() - m.actH() + 7)
	col := (x - 6) / heatCellW
	weeks := heatWeeks(m.gridW())
	if row < 0 || row > 6 || col < 0 || col >= weeks {
		return 0, 0, false
	}
	if heatDate(heatStart(weeks), col, row).After(tokens.StartOfToday()) {
		return 0, 0, false
	}
	return col, row, true
}

// clickHeat selects the heatmap day under (x,y) and enters day-navigation.
func (m *Model) clickHeat(x, y int) {
	col, row, ok := m.heatCellAt(x, y)
	if !ok {
		return
	}
	m.actCol, m.actRow = col, row
	if m.mode != "activity" {
		m.push("activity")
	}
}

// activityBody renders the focused home's daily token heatmap. While tokens are
// still being aggregated it shows a skeleton grid (the layout, all cells empty).
func (m Model) activityBody() string {
	ts, ok := m.toks[m.homes[m.cursor].Name]
	switch {
	case !ok || ts.loading:
		skel := heatmap(map[string]tokens.Stat{}, m.gridW(), -1, -1, -1, -1, false)
		return skel + "\n\n" + ui.Dim.Render("counting tokens…")
	case ts.u == nil || len(ts.u.Daily) == 0:
		return ui.Dim.Render("no activity")
	}
	return m.heatBlock(ts.u)
}

// heatBlock is the heatmap grid + legend + window summary + selected-day line.
func (m Model) heatBlock(u *tokens.Usage) string {
	gridW := m.gridW()
	active := m.mode == "activity"
	grid := heatmap(u.Daily, gridW, m.actCol, m.actRow, m.heatHoverC, m.heatHoverR, active)

	weeks := heatWeeks(gridW)
	start, today := heatStart(weeks), tokens.StartOfToday()
	var sum, busiest int64
	var busyDay time.Time
	days := 0
	for c := range weeks {
		for r := range 7 {
			d := heatDate(start, c, r)
			if d.After(today) {
				continue
			}
			if v := u.Daily[d.Format("2006-01-02")].Tokens; v > 0 {
				sum += v
				days++
				if v > busiest {
					busiest, busyDay = v, d
				}
			}
		}
	}
	avg := int64(0)
	if days > 0 {
		avg = sum / int64(days)
	}

	legend := ui.Dim.Render("less ") + heatStyles[0].Render("·")
	for _, s := range heatStyles[1:] {
		legend += " " + s.Render("■")
	}
	legend += ui.Dim.Render(" more")
	summary := ui.Dim.Render(fmt.Sprintf("%d active days · avg %s/day", days, ui.FmtTokens(avg)))
	if busiest > 0 {
		summary += ui.Dim.Render("  · busiest "+busyDay.Format("Jan 2")+" ") + ui.Cream.Render(ui.FmtTokens(busiest))
	}

	var sel string
	if active {
		d := heatDate(start, m.actCol, m.actRow)
		s := u.Daily[d.Format("2006-01-02")]
		sel = ui.Clay.Render("▸ ") + ui.Bold.Render(d.Format("Mon Jan 2, 2006")) + "   "
		if s.Tokens > 0 {
			sel += ui.Cream.Render(ui.FmtTokens(s.Tokens)) + ui.Dim.Render(" tok  ·  ") + ui.Green.Render(ui.FmtCost(s.Cost))
		} else {
			sel += ui.Dim.Render("no activity this day")
		}
	} else {
		sel = ui.Dim.Render("press ") + ui.Bold.Render("g") + ui.Dim.Render(" or click a day to explore")
	}
	return grid + "\n\n" + legend + "     " + summary + "\n" + sel
}

// ── heatmap palette + grid ────────────────────────────────────────────────────

// heatPalettes are the selectable heatmap ramps (level 0 empty … 4 most). The
// empty cell is a dark receding tone in every palette.
var heatPalettes = map[string][]string{
	"Clay":  {"#2f2e38", "#8f4a30", "#bb6446", "#D97757", "#f3ac86"},
	"Green": {"#2f2e38", "#0e4429", "#006d32", "#26a641", "#39d353"},
	"Blue":  {"#2f2e38", "#0a3069", "#1f4e9e", "#3b7dd8", "#79b8ff"},
}

func makeHeatStyles(name string) []lipgloss.Style {
	cs, ok := heatPalettes[name]
	if !ok {
		cs = heatPalettes["Clay"]
	}
	out := make([]lipgloss.Style, len(cs))
	for i, c := range cs {
		out[i] = lipgloss.NewStyle().Foreground(lipgloss.Color(c))
	}
	return out
}

// heatStyles / heatWeekCap are driven by preferences (applyPrefs).
var (
	heatStyles  = makeHeatStyles("Clay")
	heatWeekCap = 52
)

var heatCursor = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))         // bright cursor cell
var heatHover = lipgloss.NewStyle().Foreground(lipgloss.Color("#E1D5C4"))     // cream hover cell

// heatmap renders a GitHub-style contribution grid (weeks × 7 days) at the given
// content width, with 2-wide cells (block + gap). The active day cursor
// (curCol,curRow, when active) renders bright white; the hovered day
// (hovCol,hovRow) renders cream. Intensity is bucketed by quartiles of the active
// days, so the shades spread out instead of all collapsing onto the faintest level.
func heatmap(daily map[string]tokens.Stat, contentW, curCol, curRow, hovCol, hovRow int, active bool) string {
	today := tokens.StartOfToday()
	weeks := heatWeeks(contentW)
	start := heatStart(weeks)
	at := func(col, row int) time.Time { return heatDate(start, col, row) }

	var vals []int64
	for c := range weeks {
		for r := range 7 {
			if d := at(c, r); !d.After(today) {
				if v := daily[d.Format("2006-01-02")].Tokens; v > 0 {
					vals = append(vals, v)
				}
			}
		}
	}
	slices.Sort(vals)
	q := func(p float64) int64 {
		if len(vals) == 0 {
			return 0
		}
		return vals[int(p*float64(len(vals)-1))]
	}
	p25, p50, p75 := q(0.25), q(0.50), q(0.75)
	level := func(v int64) int {
		switch {
		case v <= 0:
			return 0
		case v <= p25:
			return 1
		case v <= p50:
			return 2
		case v <= p75:
			return 3
		default:
			return 4
		}
	}

	// month header at each month's first column ×cellW (skip the partial leading
	// month and any label too close to the previous one)
	mline := []rune(strings.Repeat(" ", weeks*heatCellW))
	lastMonth, lastCol := at(0, 0).Format("Jan"), -99
	for c := 1; c < weeks; c++ {
		if mn := at(c, 0).Format("Jan"); mn != lastMonth {
			lastMonth = mn
			if c >= lastCol+2 {
				for k := 0; k < 3 && c*heatCellW+k < len(mline); k++ {
					mline[c*heatCellW+k] = rune(mn[k])
				}
				lastCol = c
			}
		}
	}
	var b strings.Builder
	b.WriteString(strings.Repeat(" ", heatLabelW) + ui.Dim.Render(string(mline)) + "\n")

	labels := [7]string{"Mon", "", "Wed", "", "Fri", "", ""}
	for r := range 7 {
		b.WriteString(ui.Dim.Render(ui.Pad(labels[r], heatLabelW)))
		for c := range weeks {
			d := at(c, r)
			switch {
			case d.After(today):
				b.WriteString("  ")
			case active && c == curCol && r == curRow:
				b.WriteString(heatCursor.Render("■") + " ")
			case c == hovCol && r == hovRow:
				b.WriteString(heatHover.Render("■") + " ")
			default:
				b.WriteString(heatStyles[level(daily[d.Format("2006-01-02")].Tokens)].Render("■") + " ")
			}
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
