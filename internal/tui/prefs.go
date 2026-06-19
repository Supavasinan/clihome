package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"clihome/internal/ui"
	"clihome/internal/update"
)

// githubURL is the maintainer's GitHub, shown on the preferences "About" panel.
const githubURL = "github.com/supavasinan/clihome"

type prefs struct {
	Palette string `json:"palette"`
	Weeks   int    `json:"weeks"`
}

var (
	paletteOpts = []string{"Clay", "Green", "Blue"}
	weeksOpts   = []int{52, 26, 13}
	weeksLabel  = map[int]string{52: "1 year", 26: "6 months", 13: "3 months"}
)

// weeksLabels lists the activity-range option labels in weeksOpts order — the
// single source of truth shared by the renderer and the hit-tester.
func weeksLabels() []string {
	out := make([]string, len(weeksOpts))
	for i, w := range weeksOpts {
		out[i] = weeksLabel[w]
	}
	return out
}

// prefRowLabelW is the width of a preferences row's label column; prefRowOptX is
// the screen column where the first option chip's text begins (pane border+pad +
// the 2-cell caret gutter + the label column + the leading separator space).
const (
	prefRowLabelW = 16
	prefRowOptX   = 2 + 2 + prefRowLabelW + 1
)

// themeAccent is the app-wide accent color for each theme.
var themeAccent = map[string]string{"Clay": "#D97757", "Green": "#2ea043", "Blue": "#388bfd"}

func prefsFile() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".clihome", "config.json")
}

func loadPrefs() prefs {
	p := prefs{Palette: "Clay", Weeks: 52}
	if b, err := os.ReadFile(prefsFile()); err == nil {
		var got prefs
		if json.Unmarshal(b, &got) == nil {
			if _, ok := heatPalettes[got.Palette]; ok {
				p.Palette = got.Palette
			}
			if _, ok := weeksLabel[got.Weeks]; ok {
				p.Weeks = got.Weeks
			}
		}
	}
	return p
}

func savePrefs(p prefs) {
	f := prefsFile()
	_ = os.MkdirAll(filepath.Dir(f), 0o755)
	if b, err := json.Marshal(p); err == nil {
		_ = os.WriteFile(f, b, 0o644)
	}
}

// applyPrefs makes the whole UI reflect the preferences — the heatmap ramp, the
// activity range, and the app-wide accent (logo, headers, selection, tints).
func applyPrefs(p prefs) {
	heatStyles = makeHeatStyles(p.Palette)
	heatWeekCap = p.Weeks
	ui.SetAccent(themeAccent[p.Palette])
	homesAccent = lipgloss.NewStyle().Foreground(lipgloss.Color(heatPalettes[p.Palette][4]))
}

func (m *Model) openPrefs() { m.prefCur, m.status, m.mode = 0, "", "prefs" }

func (m Model) updatePrefs(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k":
		if m.prefCur > 0 {
			m.prefCur--
		}
	case "down", "j":
		if m.prefCur < 1 {
			m.prefCur++
		}
	case "left", "h":
		m.cyclePref(m.prefCur, -1)
	case "right", "l", " ":
		m.cyclePref(m.prefCur, 1)
	case "esc", "q", "ctrl+c":
		savePrefs(m.prefs)
		m.mode = "browse"
	}
	return m, nil
}

func (m *Model) cyclePref(field, dir int) {
	switch field {
	case 0:
		i := (slices.Index(paletteOpts, m.prefs.Palette) + dir + len(paletteOpts)) % len(paletteOpts)
		m.setPref(field, i)
	case 1:
		i := (slices.Index(weeksOpts, m.prefs.Weeks) + dir + len(weeksOpts)) % len(weeksOpts)
		m.setPref(field, i)
	}
}

// setPref applies and persists a preference value immediately.
func (m *Model) setPref(field, idx int) {
	switch field {
	case 0:
		m.prefs.Palette = paletteOpts[idx]
	case 1:
		m.prefs.Weeks = weeksOpts[idx]
	}
	applyPrefs(m.prefs)
	savePrefs(m.prefs)
}

// prefHitAt maps (x,y) to a preferences (field, option) pair, or (-1,-1). The
// Theme row is at screen row 6, the Activity-range row at 8; each option is the
// chip " <opt> " laid out from prefRowOptX, separated by a single space. Shared by
// clickPrefs and hover so the highlight tracks exactly where a click would land.
func (m Model) prefHitAt(x, y int) (int, int) {
	var opts []string
	field := -1
	switch y {
	case 6:
		field, opts = 0, paletteOpts
	case 8:
		field, opts = 1, weeksLabels()
	}
	if field < 0 {
		return -1, -1
	}
	pos := prefRowOptX
	for i, o := range opts {
		if w := len(o) + 2; x >= pos && x < pos+w {
			return field, i
		} else {
			pos += w + 1 // chip width + the separator space before the next chip
		}
	}
	return -1, -1
}

// clickPrefs sets the preference option under (x,y).
func (m *Model) clickPrefs(x, y int) {
	field, opt := m.prefHitAt(x, y)
	if field < 0 {
		return
	}
	m.prefCur = field
	m.setPref(field, opt)
}

// prefsView renders the preferences page: the interactive Theme + Activity-range
// rows, then a non-interactive About panel with the project's GitHub link.
func (m Model) prefsView() string {
	header := m.header()
	footer := "  " + ui.Dim.Render("↑/↓ field · ←/→ change · esc save & back")

	row := func(field int, label string, opts []string, cur string, sel bool) string {
		s := ui.Cursor(sel)
		s += ui.Dim.Render(ui.Pad(label, prefRowLabelW))
		for i, o := range opts {
			switch {
			case o == cur:
				s += " " + ui.Bar.Render(" "+o+" ")
			case field == m.prefHoverField && i == m.prefHoverOpt:
				s += " " + ui.Cream.Render(" "+o+" ")
			default:
				s += " " + ui.Dim.Render(" "+o+" ")
			}
		}
		return s + "\n"
	}
	wlabels := weeksLabels()
	var b strings.Builder
	b.WriteString(row(0, "Theme", paletteOpts, m.prefs.Palette, m.prefCur == 0) + "\n")
	b.WriteString(row(1, "Activity range", wlabels, weeksLabel[m.prefs.Weeks], m.prefCur == 1))

	// About — non-interactive: version, any pending update, and the GitHub link.
	b.WriteString("\n" + ui.Dim.Render(strings.Repeat("─", 30)) + "\n\n")
	b.WriteString("  " + ui.Dim.Render(ui.Pad("Version", 16)) + " " + ui.Cream.Render(ui.VersionLabel()) + "\n")
	if m.updLatest != "" {
		b.WriteString("  " + ui.Dim.Render(ui.Pad("Update", 16)) + " " +
			ui.Yellow.Render("v"+m.updLatest+" available") + "  " + ui.Dim.Render(update.UpgradeCmd) + "\n")
	}
	b.WriteString("  " + ui.Dim.Render(ui.Pad("GitHub", 16)) + " " + ui.Cyan.Render(githubURL) + "\n")

	body := ui.Pane("preferences", b.String(), max(m.w-7, 40), max(m.h-6, 8)-2)
	return ui.Screen(header, body, footer)
}
