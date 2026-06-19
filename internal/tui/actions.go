package tui

import (
	"os/exec"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"clihome/internal/home"
	"clihome/internal/ui"
)

// manageRowsTop is the screen row of the first action in the manage pane: pane
// content begins at row 6, after the info card (manageCardLines).
const (
	manageCardLines = 6 // title, divider, account/plan, usage, tokens/cost, blank
	manageRowsTop   = 6 + manageCardLines
)

// manageDivider is the muted-clay rule under the stat card's title.
var manageDivider = lipgloss.NewStyle().Foreground(lipgloss.Color("#6b4a3c"))

// twoCol lays two label/value cells side by side: left padded to half of w, then
// right, with the whole line clamped to w so a long right cell can't overflow and
// wrap (a wrapped manage line would shift the action rows below it off their
// hit-test offsets). Visible-width aware, so ANSI-styled cells stay aligned.
func twoCol(left, right string, w int) string {
	return ui.AnsiPad(ui.AnsiPad(left, max(w/2, 1))+right, w)
}

// actionItem is one row in a home's action menu. kind drives doAction; danger
// marks a destructive action (Delete) for emphasis.
type actionItem struct {
	icon, label, kind string
	danger            bool
}

// actionItems are the per-home actions, in display order. Renaming, reordering,
// or adding rows here (plus a doAction case + an actionSubs entry) is all it
// takes to extend the menu.
var actionItems = []actionItem{
	{"⇅", "Sync config", "sync", false},
	{"↺", "Restore", "restore", false},
	{"✚", "Snapshot now", "snapshot", false},
	{"≡", "Session history", "history", false},
	{"⌂", "Open folder", "open", false},
	{"»", "Launch command", "launch", false},
	{"✕", "Delete home", "delete", true},
}

// openActions opens the per-home actions menu for the focused home. These were
// previously global footer shortcuts (s sync · r restore); they now live behind
// a click or enter on a specific home, alongside more per-home actions.
func (m *Model) openActions() {
	if len(m.homes) == 0 {
		return
	}
	m.actionCur, m.confirmDelete, m.status = 0, false, ""
	m.push("actions")
}

func (m Model) updateActions(key string) (tea.Model, tea.Cmd) {
	if m.confirmDelete {
		switch key {
		case "y":
			m.deleteHome()
			return m, m.ensure()
		case "n", "esc", "q", "ctrl+c":
			m.confirmDelete = false
		}
		return m, nil
	}
	switch key {
	case "up", "k":
		if m.actionCur > 0 {
			m.actionCur--
		}
	case "down", "j":
		if m.actionCur < len(actionItems)-1 {
			m.actionCur++
		}
	case "enter", " ", "right", "l":
		m.doAction()
	case "s":
		m.runKind("sync")
	case "r":
		m.runKind("restore")
	case "o":
		m.runKind("open")
	case "d":
		m.runKind("delete")
	case "esc", "q", "ctrl+c":
		m.goBack()
		return m, nil
	}
	return m, m.afterAction()
}

// afterAction returns any command the just-run action needs — currently the
// lazy session-history fetch when Session history opens.
func (m *Model) afterAction() tea.Cmd {
	if m.mode == "history" {
		return m.ensureSessions()
	}
	return nil
}

// runKind moves the cursor to the action with the given kind and runs it (key
// accelerators).
func (m *Model) runKind(kind string) {
	for i, a := range actionItems {
		if a.kind == kind {
			m.actionCur = i
			m.doAction()
			return
		}
	}
}

// doAction runs the highlighted action.
func (m *Model) doAction() {
	h := m.homes[m.cursor]
	switch actionItems[m.actionCur].kind {
	case "sync":
		if m.fpKind == "diff" && m.fp.NChanged > 0 {
			m.startSync()
		} else {
			m.status = ui.Yellow.Render("nothing to sync — " + syncReason(m.fpKind))
		}
	case "restore":
		m.startRestore()
	case "snapshot":
		m.createRestorePoint() // snapshot current config as a fresh restore point
	case "history":
		m.openHistory()
	case "open":
		m.openFolder(h)
	case "launch":
		m.status = ui.Clay.Render("» ") + ui.Dim.Render("run: ") + ui.Cream.Render(launchLine(h))
	case "delete":
		m.confirmDelete = true
	}
}

// openFolder reveals a home's directory in the OS file manager.
func (m *Model) openFolder(h home.Home) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", h.Dir)
	case "windows":
		cmd = exec.Command("explorer", h.Dir)
	default:
		cmd = exec.Command("xdg-open", h.Dir)
	}
	if err := cmd.Start(); err != nil {
		m.status = ui.Red.Render("✗ ") + err.Error()
		return
	}
	m.status = ui.Green.Render("⌂ ") + ui.Dim.Render("opened "+shortPath(h.Dir))
}

// launchLine is the shell command that runs a tool against this home, e.g.
// "CLAUDE_CONFIG_DIR=~/.claude2 claude".
func launchLine(h home.Home) string {
	if h.Provider.Env == "" {
		return h.Provider.Command
	}
	return h.Provider.Env + "=" + shortPath(h.Dir) + " " + h.Provider.Command
}

// deleteHome moves the focused home to trash, then re-discovers and re-focuses.
func (m *Model) deleteHome() {
	h := m.homes[m.cursor]
	trash, err := home.Delete(h)
	m.confirmDelete = false
	m.goHome()
	if err != nil {
		m.status = ui.Red.Render("✗ ") + err.Error()
		return
	}
	delete(m.usage, h.Name)
	delete(m.toks, h.Name)
	m.homes = home.Discover()
	if m.cursor >= len(m.homes) {
		m.cursor = max(len(m.homes)-1, 0)
	}
	m.states = make([]string, len(m.homes))
	for i := range m.homes {
		m.states[i] = m.stateOf(i)
	}
	if len(m.homes) > 0 {
		m.recompute()
	}
	m.status = ui.Green.Render("🗑 ") + ui.Dim.Render("deleted "+h.Name+" — moved to "+shortPath(trash))
}

// actionRowAt maps (x,y) to an action-menu row index, or -1. It only matches rows
// that are actually visible: on a short terminal the manage pane clips the action
// list, so rows past what fits must not be hittable (they'd sit over the pane
// border or the activity strip). Shared by clickActions and hover so the
// highlight tracks exactly where a click would land.
func (m Model) actionRowAt(x, y int) int {
	if m.confirmDelete || x < 0 || x >= m.homesW() {
		return -1
	}
	i := y - manageRowsTop
	if i < 0 || i >= m.visibleActions() {
		return -1
	}
	return i
}

// visibleActions is how many action rows the manage pane can show at the current
// height: the pane's content rows (topH-4) minus the stat card, clamped to the
// menu length. Below this the list is clipped.
func (m Model) visibleActions() int {
	topH := m.bodyH() - m.actH()
	return min(max(topH-4-manageCardLines, 0), len(actionItems))
}

// clickActions activates the action row under (x,y).
func (m *Model) clickActions(x, y int) tea.Cmd {
	if i := m.actionRowAt(x, y); i >= 0 {
		m.actionCur = i
		m.doAction()
		return m.afterAction()
	}
	return nil
}

// syncReason explains why Sync is unavailable for the current home state.
func syncReason(kind string) string {
	switch kind {
	case "clean":
		return "already in sync with base"
	case "synced-base":
		return "this is the base; all peers in sync"
	default:
		return "no peer to sync with"
	}
}

// actionSubs returns the contextual subtitle shown beside each action, indexed
// to match actionItems.
func (m Model) actionSubs() []string {
	h := m.homes[m.cursor]
	var sync string
	switch m.fpKind {
	case "diff":
		sync = ui.Yellow.Render(ui.Plural(m.fp.NChanged, "change") + " ← " + m.fpSrc.Name)
	case "clean":
		sync = ui.Green.Render("in sync with base")
	case "synced-base":
		sync = ui.Dim.Render("base — peers in sync")
	default:
		sync = ui.Dim.Render("only home for this tool")
	}
	history := ui.Dim.Render("tokens & cost per session")
	if ts, ok := m.toks[h.Name]; ok && ts.u != nil {
		history = ui.Cream.Render(ui.FmtTokens(ts.u.Total.Tokens)) + ui.Dim.Render(" tok · ") + ui.Green.Render(ui.FmtCost(ts.u.Total.Cost))
	}
	return []string{
		sync,
		ui.Dim.Render("roll back to a saved point"),
		ui.Dim.Render("save current config as a point"),
		history,
		ui.Dim.Render(shortPath(h.Dir)),
		ui.Dim.Render(launchLine(h)),
		ui.Dim.Render("move to trash (recoverable)"),
	}
}

// actionsPanel renders the per-home action menu (design by Codex), shown in place
// of the homes list in the left pane so the focused home's details + activity
// stay visible. A refined header line, then one blank line, then six single-line
// action rows — the geometry clickActions relies on (first row at content idx 2).
func (m Model) actionsPanel() string {
	h := m.homes[m.cursor]
	// The pane's inner text area is 2 cols narrower than its outer width (border +
	// padding). Building to the outer width made the title line wrap, pushing every
	// action row down by one and throwing the click/hover hit-test off by a row.
	w := m.homesW() - 2
	if w < 1 {
		w = 1
	}

	state := stateBadge(m.states[m.cursor])
	sep := ui.Dim.Render("  ·  ")
	identity := ui.Cream.Bold(true).Render(h.Name) + sep + ui.Dim.Render(h.Provider.Label) + sep + state

	if m.confirmDelete {
		nameW := max(w-9, 0)
		name := ui.Cream.Bold(true).Render(ui.Truncate(h.Name, nameW))
		prompt := ui.AnsiPad(ui.Red.Bold(true).Render("Delete ")+name+ui.Red.Bold(true).Render("?"), w)
		trashLabel := ui.Dim.Render("moves to ")
		trash := ui.Cyan.Render(ui.Truncate(shortPath(home.TrashRoot()), max(w-lipgloss.Width(trashLabel), 0)))
		trashLine := ui.AnsiPad(trashLabel+trash, w)
		note := ui.AnsiPad(ui.Dim.Render("recoverable — moved, not erased"), w)
		affordance := ui.AnsiPad(ui.Bar.Render(" y ")+ui.Dim.Render(" delete · ")+ui.Bold.Render("n")+ui.Dim.Render(" cancel"), w)
		return identity + "\n\n" + prompt + "\n" + trashLine + "\n" + note + "\n\n" + affordance
	}

	// stat card (manageCardLines tall) — a title row with a state pill, a clay
	// divider, then a 2-column grid: account/plan, usage gauges, tokens/cost.
	plan := h.Provider.Plan(h.Dir)
	if st, ok := m.usage[h.Name]; ok && st.u != nil && st.u.Plan != "" {
		plan = st.u.Plan
	}
	tok, cost := m.totalTokCost(h.Name)
	lbl := func(s string) string { return ui.Dim.Render(s + "  ") }
	pill := ui.Clay.Render("▎") + state + ui.Clay.Render("▎")
	title := ui.Cream.Bold(true).Render(h.Name) + ui.Dim.Render("  ·  ") + ui.Dim.Render(h.Provider.Label)
	titleLine := ui.AnsiPad(title, max(w-lipgloss.Width(pill), 0)) + pill
	div := manageDivider.Render(strings.Repeat("─", min(max(w, 4), 58)))
	acctLine := twoCol(lbl("ACCOUNT")+ui.Cyan.Render(ui.OrDash(h.Provider.Email(h.Dir))), lbl("PLAN")+ui.Cream.Render(ui.OrDash(plan)), w)
	usageLine := twoCol(lbl("SESSION")+m.usageBar(h.Name, 0), lbl("WEEKLY")+m.usageBar(h.Name, 1), w)
	spendLine := twoCol(lbl("TOKENS")+ui.Cream.Render(tok), lbl("COST")+ui.Green.Render(cost), w)
	header := titleLine + "\n" + div + "\n" + acctLine + "\n" + usageLine + "\n" + spendLine

	subs := m.actionSubs()
	labelW := actionLabelWidth(w)
	rows := ""
	for i, a := range actionItems {
		if i > 0 {
			rows += "\n"
		}
		rows += actionRow(w, labelW, i == m.actionCur, i == m.actionHover, a.icon, a.label, subs[i], a.danger)
	}
	return header + "\n\n" + rows
}

// actionLabelWidth scales the label column to the pane width.
func actionLabelWidth(w int) int {
	switch {
	case w < 34:
		return 16
	case w < 48:
		return 19
	default:
		return 23
	}
}

// actionRow renders one action: a pointer + icon + label column + aligned
// subtitle. The selected row is a full-width highlight bar (red for destructive)
// carrying plain text — inner color resets would otherwise break the fill. A
// hovered (not-selected) row shows the cream hover gutter instead of the blank
// one, matching the affordance on the homes table.
func actionRow(w, labelW int, selected, hovered bool, icon, label, sub string, danger bool) string {
	if w < 1 {
		return ""
	}
	const pointerW, iconW, minSubW = 2, 3, 6
	gapW, subW := 2, 0
	remainingW := w - pointerW - iconW
	if remainingW <= 14 {
		labelW, gapW = remainingW, 0
	} else {
		labelW = min(labelW, remainingW-gapW-minSubW)
		labelW = max(labelW, 8)
		subW = remainingW - labelW - gapW
	}
	labelW = max(labelW, 0)
	subW = max(subW, 0)
	gap := ui.Pad("", gapW)
	label = ui.Truncate(label, labelW)

	if selected {
		text := "❯ " + ui.Pad(icon, iconW) + ui.Pad(label, labelW) + gap + ui.Pad(ui.Strip(sub), subW)
		bar := ui.Bar
		if danger {
			bar = ui.BarDanger
		}
		return bar.Render(ui.AnsiPad(text, w))
	}

	iconStyle, labelStyle := ui.Dim, ui.Cream
	if danger {
		iconStyle, labelStyle = ui.Red, ui.Red
	}
	gutter := "  "
	if hovered {
		gutter = ui.HoverGutter()
	}
	row := gutter + ui.AnsiPad(iconStyle.Render(icon), iconW) + ui.AnsiPad(labelStyle.Render(label), labelW) + gap + ui.AnsiPad(sub, subW)
	return ui.AnsiPad(row, w)
}
