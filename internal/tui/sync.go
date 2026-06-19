package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"clihome/internal/diff"
	"clihome/internal/plan"
	"clihome/internal/syncer"
	"clihome/internal/ui"
)

// startSync collects the focused home's changed entries into a process, selects
// them all, and opens the file-selection + diff screen.
func (m *Model) startSync() {
	m.syncEntries = nil
	for _, e := range m.fp.Entries {
		if e.State == "change" {
			m.syncEntries = append(m.syncEntries, e)
		}
	}
	m.procTitle, m.procSrc, m.procDst, m.procDstName = "sync", m.fpSrc.Name, m.fpDst.Name, m.fpDst.Name
	m.selectAllAndOpen()
}

// startRestoreProcess opens the file-selection + diff process for a restore
// point, reusing the same UI as sync (snapshot → live home).
func (m *Model) startRestoreProcess(rp syncer.RestorePoint) {
	h := m.homes[m.cursor]
	entries := syncer.RestorePlan(rp, h.Dir).Entries
	if len(entries) == 0 {
		m.status = ui.Yellow.Render(h.Name + " already matches this point — nothing to restore")
		return // stay in the restore panel
	}
	m.syncEntries = entries
	m.procTitle, m.procSrc, m.procDst, m.procDstName = "restore", stamp(rp.Stamp), h.Name, h.Name
	m.selectAllAndOpen()
}

// selectAllAndOpen selects every entry and enters the process screen.
func (m *Model) selectAllAndOpen() {
	m.syncSel = make([]bool, len(m.syncEntries))
	for i := range m.syncSel {
		m.syncSel[i] = true
	}
	m.syncCursor, m.diffScroll, m.status = 0, 0, ""
	m.push("sync")
}

func (m Model) updateSync(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up":
		if m.syncCursor > 0 {
			m.syncCursor--
			m.diffScroll = 0
		}
	case "down":
		if m.syncCursor < len(m.syncEntries)-1 {
			m.syncCursor++
			m.diffScroll = 0
		}
	case "j":
		m.diffScroll = m.clampScroll(m.diffScroll + 1)
	case "k":
		m.diffScroll = m.clampScroll(m.diffScroll - 1)
	case "pgdown", "ctrl+d":
		m.diffScroll = m.clampScroll(m.diffScroll + 8)
	case "pgup", "ctrl+u":
		m.diffScroll = m.clampScroll(m.diffScroll - 8)
	case " ":
		m.syncSel[m.syncCursor] = !m.syncSel[m.syncCursor]
	case "a":
		all := true
		for _, v := range m.syncSel {
			all = all && v
		}
		for i := range m.syncSel {
			m.syncSel[i] = !all
		}
	case "enter":
		m.applySelectedSync()
	case "esc", "q", "ctrl+c":
		m.goBack()
	}
	return m, nil
}

// clampScroll keeps the diff scroll within the highlighted entry's bounds.
func (m Model) clampScroll(v int) int {
	rowsH := max(m.h-6, 8) - 4
	return ui.ClampScroll(v, len(m.entryDiffLines(m.syncCursor)), rowsH)
}

// applySelectedSync applies only the selected entries of the active process
// (sync or restore) onto the target, writing a restore point for it first.
func (m *Model) applySelectedSync() {
	m.goHome()
	var fp plan.Plan
	for i, e := range m.syncEntries {
		if m.syncSel[i] {
			fp.Entries = append(fp.Entries, e)
			fp.NChanged++
			fp.Files += e.Nw + e.Ch
		}
	}
	if fp.NChanged == 0 {
		m.status = ui.Yellow.Render("nothing selected — nothing applied")
		return
	}
	rp := filepath.Join(syncer.RestoreRoot(), syncer.Timestamp(), m.procDstName)
	n, err := syncer.Apply(fp, rp)
	verb, icon := "synced", ui.Green.Render("✓ ")
	if m.procTitle == "restore" {
		verb, icon = "restored", ui.Green.Render("↺ ")
	}
	if err != nil {
		m.status = ui.Red.Render("✗ ") + err.Error()
	} else {
		m.status = icon + ui.Dim.Render(
			fmt.Sprintf("%s %d file(s)  %s → %s  ·  restore point %s", verb, n, m.procSrc, m.procDst, shortPath(rp)))
	}
	for i := range m.homes {
		m.states[i] = m.stateOf(i)
	}
	m.recompute()
}

// syncDiffW is the drawable column width of the sync diff pane (its inner text
// width minus the reserved scrollbar column). Shared by the renderer and the
// line-counter so scrolling and image sizing stay consistent.
func (m Model) syncDiffW() int {
	innerW := max(m.w-7, 40)
	leftW := innerW * 42 / 100
	rightW := innerW - leftW
	return rightW - 3
}

// syncView is the sync process: file checkboxes (left) + live scrollable diff (right).
func (m Model) syncView() string {
	header := m.header()

	sel, files := 0, 0
	for i, e := range m.syncEntries {
		if m.syncSel[i] {
			sel++
			files += e.Nw + e.Ch + e.Rm
		}
	}
	footer := "  " + ui.Dim.Render(fmt.Sprintf("%d/%d selected · %s", sel, len(m.syncEntries), ui.Plural(files, "file"))) + "   " +
		ui.Dim.Render("↑/↓ file · j/k·pgup/dn scroll · space pick · a all · ") +
		ui.Bold.Render("enter") + ui.Dim.Render(" "+m.procTitle+" · esc cancel")

	innerW := max(m.w-7, 40)
	leftW := innerW * 42 / 100
	rightW := innerW - leftW
	paneH := max(m.h-6, 8) - 2
	rowsH := paneH - 2 // minus title + blank

	// left: file selection list
	var lb strings.Builder
	for i, e := range m.syncEntries {
		box := ui.Dim.Render("○")
		if m.syncSel[i] {
			box = ui.Clay.Render("◉")
		}
		nameStyle := ui.Dim
		if i == m.syncCursor {
			nameStyle = ui.Bold
		}
		name := ui.Truncate(e.Rel, leftW-12)
		lb.WriteString(ui.Cursor(i == m.syncCursor) + box + " " + nameStyle.Render(name) + "  " + entrySummary(e) + "\n")
	}

	// right: scrollable diff of the highlighted entry, with a scrollbar
	diffBody := ui.ScrollView(m.entryDiffLines(m.syncCursor), m.syncDiffW(), rowsH, m.diffScroll)

	leftPane := ui.Pane("files", lb.String(), leftW, paneH)
	rightPane := ui.Pane("diff", diffBody, rightW, paneH)
	body := lipgloss.JoinHorizontal(lipgloss.Top, leftPane, "  ", rightPane)
	return ui.Screen(header, body, footer)
}

// entryDiffLines renders the line-level diff for one entry (its files), as lines.
// Image files are rendered inline as half-block art, sized to the diff pane.
func (m Model) entryDiffLines(i int) []string {
	if i >= len(m.syncEntries) {
		return nil
	}
	w := m.syncDiffW()
	var out []string
	e := m.syncEntries[i]
	for k, u := range e.Units {
		if k > 0 {
			out = append(out, "")
		}
		if len(e.Units) > 1 {
			out = append(out, ui.Cream.Render(u.Rel)+"  "+ui.Dim.Render("("+u.Kind+")"))
		}
		var oldB, newB []byte
		if fileExists(u.Dst) {
			oldB = readForDiff(u.Dst)
		}
		if u.Src != "" {
			newB = readForDiff(u.Src)
		}
		out = append(out, strings.Split(diff.Format(oldB, newB, w), "\n")...)
	}
	return out
}
