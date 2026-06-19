package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"clihome/internal/home"
	"clihome/internal/provider"
	"clihome/internal/ui"
)

func (m *Model) startNew() { m.newCur, m.status, m.mode = 0, "", "new" }

func (m Model) updateNew(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k":
		if m.newCur > 0 {
			m.newCur--
		}
	case "down", "j":
		if m.newCur < len(provider.All())-1 {
			m.newCur++
		}
	case "enter":
		m.createNew()
		return m, m.ensure()
	case "esc", "q", "ctrl+c":
		m.mode = "browse"
	}
	return m, nil
}

// createNew scaffolds the next config home for the selected provider, then
// re-discovers and focuses it.
func (m *Model) createNew() {
	p := provider.All()[m.newCur]
	h, err := home.Create(p)
	if err != nil {
		m.status = ui.Red.Render("✗ ") + err.Error()
		m.mode = "browse"
		return
	}
	m.homes = home.Discover()
	m.states = make([]string, len(m.homes))
	for i := range m.homes {
		m.states[i] = m.stateOf(i)
		if m.homes[i].Name == h.Name {
			m.cursor = i
		}
	}
	m.recompute()
	m.status = ui.Green.Render("✚ ") + ui.Dim.Render("created "+h.Name+" — open it to sync config in")
	m.mode = "browse"
}

// newRowAt maps a screen row y to a provider-picker index, or -1; rows start at
// screen row 8 (border + title + blank + prompt + blank). The whole row is the
// click target, so x is irrelevant. Shared by clickNew and hover.
func (m Model) newRowAt(y int) int {
	if i := y - 8; i >= 0 && i < len(provider.All()) {
		return i
	}
	return -1
}

// clickNew picks and creates the provider row under (x,y).
func (m *Model) clickNew(x, y int) {
	if i := m.newRowAt(y); i >= 0 {
		m.newCur = i
		m.createNew()
	}
}

// newView is the provider picker for creating a new config home.
func (m Model) newView() string {
	header := m.header()
	footer := "  " + ui.Dim.Render("↑/↓ tool · ") + ui.Bold.Render("enter") + ui.Dim.Render(" create · esc cancel")
	var b strings.Builder
	b.WriteString(ui.Dim.Render("create a new config home for:") + "\n\n")
	for i, p := range provider.All() {
		switch {
		case i == m.newCur:
			b.WriteString(ui.Cursor(true) + ui.Tint(p.ID == "codex").Bold(true).Render(p.Label) + "\n")
		case i == m.newHover:
			b.WriteString(ui.CursorState(false, true) + ui.Cream.Render(p.Label) + "\n")
		default:
			b.WriteString(ui.Cursor(false) + ui.Dim.Render(p.Label) + "\n")
		}
	}
	body := ui.Pane("new home", b.String(), max(m.w-7, 40), max(m.h-6, 8)-2)
	return ui.Screen(header, body, footer)
}
