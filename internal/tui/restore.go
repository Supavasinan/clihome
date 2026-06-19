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

// startRestore opens the restore-point panel for the focused home (always —
// even with no points yet, so a fresh one can be created with c).
func (m *Model) startRestore() {
	m.restorePts = syncer.RestorePoints(m.homes[m.cursor].Name)
	m.restoreCur, m.restoreFileCur, m.diffScroll, m.status = 0, 0, 0, ""
	m.push("restore")
}

// createRestorePoint snapshots the focused home's current config as a new
// restore point, so it can be rolled back to later.
func (m *Model) createRestorePoint() {
	h := m.homes[m.cursor]
	snap := filepath.Join(syncer.RestoreRoot(), syncer.Timestamp(), h.Name)
	p := plan.Sync(h.Dir, snap, h.Provider.Manifest, h.Provider.Deny, false, false)
	n, err := syncer.Apply(p, "") // copy current files into the snapshot dir
	if err != nil {
		m.status = ui.Red.Render("✗ ") + err.Error()
	} else {
		m.status = ui.Green.Render("✚ ") + ui.Dim.Render(
			fmt.Sprintf("restore point created — %s · %s", h.Name, ui.Plural(n, "file")))
	}
	m.restorePts = syncer.RestorePoints(h.Name)
	m.restoreCur, m.restoreFileCur, m.diffScroll = 0, 0, 0
}

func (m Model) updateRestore(key string) (tea.Model, tea.Cmd) {
	files := m.restoreFiles()
	switch key {
	case "left", "h":
		if m.restoreCur > 0 {
			m.restoreCur--
			m.restoreFileCur, m.diffScroll = 0, 0
		}
	case "right", "l":
		if m.restoreCur < len(m.restorePts)-1 {
			m.restoreCur++
			m.restoreFileCur, m.diffScroll = 0, 0
		}
	case "up":
		if m.restoreFileCur > 0 {
			m.restoreFileCur--
			m.diffScroll = 0
		}
	case "down":
		if m.restoreFileCur < len(files)-1 {
			m.restoreFileCur++
			m.diffScroll = 0
		}
	case "k":
		m.diffScroll = m.restoreClamp(m.diffScroll - 1)
	case "j":
		m.diffScroll = m.restoreClamp(m.diffScroll + 1)
	case "pgup", "ctrl+u":
		m.diffScroll = m.restoreClamp(m.diffScroll - 8)
	case "pgdown", "ctrl+d":
		m.diffScroll = m.restoreClamp(m.diffScroll + 8)
	case "c":
		m.createRestorePoint()
	case "enter":
		if len(m.restorePts) > 0 {
			m.startRestoreProcess(m.restorePts[m.restoreCur])
		}
	case "esc", "q", "ctrl+c":
		m.goBack()
	}
	return m, nil
}

// restoreFiles returns the files captured in the highlighted restore point.
func (m Model) restoreFiles() []syncer.RestoreFile {
	if len(m.restorePts) == 0 {
		return nil
	}
	return syncer.RestoreFileList(m.restorePts[m.restoreCur])
}

// restoreDiffW is the drawable column width of the restore diff pane.
func (m Model) restoreDiffW() int {
	innerW := max(m.w-10, 45)
	pointsW := innerW * 26 / 100
	filesW := innerW * 32 / 100
	diffW := innerW - pointsW - filesW
	return diffW - 3
}

// restoreDiffLines renders the diff for the highlighted file in the highlighted
// restore point: the current live file (old) → the snapshot's version (new) —
// i.e. what restoring this file would change, the same direction the restore
// apply-flow uses. Files the sync added have no snapshot copy, so they read as
// fully removed.
func (m Model) restoreDiffLines() []string {
	files := m.restoreFiles()
	if m.restoreFileCur >= len(files) {
		return nil
	}
	f := files[m.restoreFileCur]
	rp := m.restorePts[m.restoreCur]
	h := m.homes[m.cursor]
	var oldB, newB []byte
	if live := filepath.Join(h.Dir, f.Rel); fileExists(live) {
		oldB = readForDiff(live)
	}
	if !f.Added {
		if snap := filepath.Join(rp.Dir, f.Rel); fileExists(snap) {
			newB = readForDiff(snap)
		}
	}
	return strings.Split(diff.Format(oldB, newB, m.restoreDiffW()), "\n")
}

// restoreClamp keeps the diff-pane scroll within the highlighted file's bounds.
func (m Model) restoreClamp(v int) int {
	rowsH := max(m.h-6, 8) - 5 // pane rows minus the filename header line
	return ui.ClampScroll(v, len(m.restoreDiffLines()), rowsH)
}

// restoreView is a 3-pane master-detail: restore points (left) · the files
// captured in the highlighted point (middle) · a live diff of the highlighted
// file (right), current → that point. ←/→ switches point, ↑/↓ moves the file
// cursor, j/k scrolls the diff; enter opens the apply flow, c snapshots now.
func (m Model) restoreView() string {
	header := m.header()
	footer := "  " + ui.Dim.Render("←/→ point · ↑/↓ file · j/k scroll · ") +
		ui.Bold.Render("enter") + ui.Dim.Render(" restore · ") +
		ui.Bold.Render("c") + ui.Dim.Render(" create · esc back")
	if m.status != "" {
		footer = "  " + m.status + "\n" + footer
	}

	innerW := max(m.w-10, 45)
	pointsW := innerW * 26 / 100
	filesW := innerW * 32 / 100
	diffW := innerW - pointsW - filesW
	paneH := max(m.h-6, 8) - 2
	rowsH := paneH - 2

	// left: restore points (two lines each: stamp + file count), windowed so the
	// highlighted point stays visible
	var pb strings.Builder
	if len(m.restorePts) == 0 {
		pb.WriteString(ui.Dim.Render("none yet — press ") + ui.Bold.Render("c") + ui.Dim.Render(" to create one"))
	}
	ppl := max(rowsH/2, 1) // points per pane (each takes 2 lines)
	poff := 0
	if len(m.restorePts) > ppl && m.restoreCur >= ppl {
		poff = m.restoreCur - ppl + 1
	}
	for i := poff; i < len(m.restorePts) && i < poff+ppl; i++ {
		rp := m.restorePts[i]
		when := ui.Truncate(stamp(rp.Stamp), pointsW-2)
		count := ui.Dim.Render("  " + ui.Plural(rp.Files, "file"))
		whenStyle := ui.Dim
		if i == m.restoreCur {
			whenStyle = ui.Bold
		}
		pb.WriteString(ui.Cursor(i == m.restoreCur) + whenStyle.Render(when) + "\n" + count + "\n")
	}

	// middle: files in the highlighted point, cursor-driven (− = a file the sync
	// added, removed on restore), windowed to keep the cursor visible
	files := m.restoreFiles()
	var fb strings.Builder
	foff := ui.TableWindow(len(files), rowsH, m.restoreFileCur)
	for i := foff; i < len(files) && i < foff+rowsH; i++ {
		f := files[i]
		name := ui.Truncate(f.Rel, filesW-4)
		switch {
		case i == m.restoreFileCur:
			ns := ui.Bold
			if f.Added {
				ns = ui.Red.Bold(true)
			}
			fb.WriteString(ui.Cursor(true) + ns.Render(name) + "\n")
		case f.Added:
			fb.WriteString(ui.Red.Render("− ") + ui.Dim.Render(name) + "\n")
		default:
			fb.WriteString(ui.Cursor(false) + ui.Cream.Render(name) + "\n")
		}
	}

	// right: a fixed filename header + the scrollable, scrollbar-tracked diff of
	// the highlighted file
	textW := diffW - 2 // pane inner text width
	var fname string
	if m.restoreFileCur < len(files) {
		fname = files[m.restoreFileCur].Rel
	}
	fileHdr := ui.AnsiPad(ui.Cream.Render(ui.Truncate(fname, textW)), textW)
	diffBody := ui.ScrollView(m.restoreDiffLines(), textW-1, rowsH-1, m.diffScroll)

	pointsPane := ui.Pane("restore points", pb.String(), pointsW, paneH)
	filesPane := ui.Pane("files", fb.String(), filesW, paneH)
	diffPane := ui.Pane("diff", fileHdr+"\n"+diffBody, diffW, paneH)
	body := lipgloss.JoinHorizontal(lipgloss.Top, pointsPane, "  ", filesPane, "  ", diffPane)
	return ui.Screen(header, body, footer)
}
