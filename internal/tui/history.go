package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"clihome/internal/home"
	"clihome/internal/tokens"
	"clihome/internal/ui"
)

// sessionColCount is how many columns the session table has.
const sessionColCount = 6

// sessState is the per-home result of a session-history fetch.
type sessState struct {
	loading bool
	s       []tokens.Session
	err     error
}

// sessMsg delivers a completed session-history fetch back to Update.
type sessMsg struct {
	home string
	s    []tokens.Session
	err  error
}

// fetchSessions lists the home's sessions off the UI thread.
func fetchSessions(h home.Home) tea.Cmd {
	return func() tea.Msg {
		s, err := h.Provider.Sessions(h.Dir)
		return sessMsg{home: h.Name, s: s, err: err}
	}
}

// ensureSessions kicks off the focused home's session fetch if not yet done.
func (m *Model) ensureSessions() tea.Cmd {
	if len(m.homes) == 0 {
		return nil
	}
	h := m.homes[m.cursor]
	if _, done := m.sessions[h.Name]; done {
		return nil
	}
	m.sessions[h.Name] = sessState{loading: true}
	return fetchSessions(h)
}

// openHistory enters the session-history list for the focused home.
func (m *Model) openHistory() {
	m.sessCursor, m.sessScroll, m.status = 0, 0, ""
	m.push("history")
}

func (m Model) updateHistory(key string) (tea.Model, tea.Cmd) {
	n := len(m.currentSessions())
	switch key {
	case "up", "k":
		if m.sessCursor > 0 {
			m.sessCursor--
		}
	case "down", "j":
		if m.sessCursor < n-1 {
			m.sessCursor++
		}
	case "pgup", "ctrl+u":
		m.sessCursor = max(m.sessCursor-10, 0)
	case "pgdown", "ctrl+d":
		m.sessCursor = min(m.sessCursor+10, max(n-1, 0))
	case "home", "g":
		m.sessCursor = 0
	case "end", "G":
		m.sessCursor = max(n-1, 0)
	case "1", "2", "3", "4", "5", "6":
		m.sortBy(int(key[0] - '1')) // sort by column N
	case "esc", "q", "ctrl+c":
		m.goBack()
	}
	return m, nil
}

// sortBy re-sorts the session table by column i via the shared table component
// (text columns default A→Z, the rest newest/largest first).
func (m *Model) sortBy(i int) {
	m.sess.Toggle(i, sessionColCount, i == 0 || i == 1)
	m.sessCursor = 0
}

// sessionCols is the session table's column set for content width w. The sort
// arrow + active/hover styling are added by the component's SortHeader.
func (m Model) sessionCols(w int) []ui.Col {
	idW, msgW, tokW, costW, lastW := 9, 6, 8, 10, 8
	projW := max(w-idW-msgW-tokW-costW-lastW-5*ui.TableGapW, 8)
	return []ui.Col{
		{Title: "SESSION", Width: idW},
		{Title: "PROJECT", Width: projW},
		{Title: "MSGS", Width: msgW, Right: true},
		{Title: "TOKENS", Width: tokW, Right: true},
		{Title: "COST", Width: costW, Right: true},
		{Title: "LAST", Width: lastW},
	}
}

// sessionColAt maps a screen x on the header row to a column index, or -1. The
// session pane spans the full width; its content starts at x=2 (border + pad).
func (m Model) sessionColAt(x int) int {
	return ui.ColAt(m.sessionCols(m.innerW()-2), 2, x)
}

// clickHistory sorts by the header column under (x,y); the header row sits at
// screen y=6 (border + title + blank).
func (m *Model) clickHistory(x, y int) {
	if y == 6 {
		if i := m.sessionColAt(x); i >= 0 {
			m.sortBy(i)
		}
	}
}

// currentSessions returns the focused home's loaded sessions, or nil.
func (m Model) currentSessions() []tokens.Session {
	if len(m.homes) == 0 {
		return nil
	}
	return m.sessions[m.homes[m.cursor].Name].s
}

// historyView lists the focused home's sessions with per-session tokens + cost,
// most-recent first, plus a grand total.
func (m Model) historyView() string {
	header := m.header()
	footer := "  " + ui.Dim.Render("↑/↓ session · pgup/dn · esc back")

	innerW := m.innerW()
	paneH := m.bodyH() - 2

	st, ok := m.sessions[m.homes[m.cursor].Name]
	var body string
	switch {
	case !ok || st.loading:
		body = ui.Dim.Render("counting sessions…")
	case st.err != nil:
		body = ui.Dim.Render("sessions unavailable — " + st.err.Error())
	case len(st.s) == 0:
		body = ui.Dim.Render("no sessions yet for this home")
	default:
		body = m.sessionList(st.s, innerW-2, paneH-6)
		footer = "  " + ui.Dim.Render("↑/↓ session · click/1-6 sort · esc back") + "   " +
			ui.Dim.Render(ui.Plural(len(st.s), "session"))
	}

	pane := ui.Pane("sessions", body, innerW, paneH)
	return ui.Screen(header, pane, footer)
}

// sessionList renders the session table via the shared ui table component:
// build rows (with per-column sort keys), let the component sort + render the
// sortable header, window to keep the cursor visible, then a grand total.
func (m Model) sessionList(ss []tokens.Session, w, rowsH int) string {
	cols := m.sessionCols(w)

	rows := make([][]ui.Cell, len(ss))
	for i, s := range ss {
		msgs := "—"
		if s.Msgs > 0 {
			msgs = itoa(s.Msgs)
		}
		rows[i] = []ui.Cell{
			{Text: shortID(s.ID), Style: ui.Cream, Key: ui.TextKey(s.ID)},
			{Text: ui.OrDash(s.Label), Style: ui.Dim, Key: ui.TextKey(s.Label)},
			{Text: msgs, Style: ui.Dim, Key: ui.NumKey(float64(s.Msgs))},
			{Text: ui.FmtTokens(s.Tokens), Style: ui.Cream, Key: ui.NumKey(float64(s.Tokens))},
			{Text: ui.FmtCost(s.Cost), Style: ui.Green, Key: ui.NumKey(s.Cost)},
			{Text: ui.RelTime(s.Modified), Style: ui.Dim, Key: ui.NumKey(float64(s.Modified.Unix()))},
		}
	}
	rows = ui.SortRows(rows, m.sess)

	cur := min(m.sessCursor, max(len(rows)-1, 0))
	off := ui.TableWindow(len(rows), rowsH, cur)

	var b strings.Builder
	b.WriteString(ui.SortHeader(cols, m.sess) + "\n\n")
	for i := off; i < len(rows) && i < off+rowsH; i++ {
		if i == cur {
			b.WriteString(ui.TableRowBar(cols, rows[i], w) + "\n")
		} else {
			b.WriteString(ui.TableRow(cols, rows[i]) + "\n")
		}
	}

	// grand total across all sessions
	var sumTok int64
	var sumCost float64
	for _, s := range ss {
		sumTok += s.Tokens
		sumCost += s.Cost
	}
	total := []ui.Cell{
		{}, {},
		{Text: "total", Style: ui.Dim},
		{Text: ui.FmtTokens(sumTok), Style: ui.Cream.Bold(true)},
		{Text: ui.FmtCost(sumCost), Style: ui.Green.Bold(true)},
		{},
	}
	b.WriteString("\n" + ui.TableRow(cols, total) + "\n")
	return b.String()
}

// shortID trims a session id to its first segment for display.
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}
