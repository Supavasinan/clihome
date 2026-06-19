package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"clihome/internal/ui"
	"clihome/internal/update"
)

// updateMsg carries the result of the background release check. latest is the
// newer version available, or "" when up to date / offline / unknown.
type updateMsg struct{ latest string }

// checkUpdate runs the release check off the UI thread (it may hit the network,
// bounded by a short timeout and a 24h cache). It posts updateMsg on completion.
func checkUpdate() tea.Cmd {
	return func() tea.Msg {
		if latest, newer := update.Check(ui.Version); newer {
			return updateMsg{latest: latest}
		}
		return updateMsg{}
	}
}
