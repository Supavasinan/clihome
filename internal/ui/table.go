package ui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// TableGap separates columns; TableGapW is its width (for column budgeting).
const (
	tableGap  = "  "
	TableGapW = len(tableGap)
)

// Col is a table column: a header, a fixed width, and alignment.
type Col struct {
	Title string
	Width int
	Right bool // right-align the header + cells (for numbers)
}

// Cell is one value in a row: plain text plus the style to paint it with. The
// style is applied after alignment, and dropped entirely on a selected row so
// the highlight bar fills cleanly. Key is the value the column sorts on.
type Cell struct {
	Text  string
	Style lipgloss.Style
	Key   SortKey
}

// SortKey is a cell's sort value — numeric where it makes sense, else textual.
type SortKey struct {
	text  string
	num   float64
	isNum bool
}

// TextKey / NumKey build a cell's sort key.
func TextKey(s string) SortKey { return SortKey{text: s} }
func NumKey(n float64) SortKey { return SortKey{num: n, isNum: true} }

func (k SortKey) less(o SortKey) bool {
	if k.isNum || o.isNum {
		return k.num < o.num
	}
	return k.text < o.text
}

// Sort is a table's sort state: the active column, direction, and the header
// column currently under the mouse (-1 = none). It lives here, not in each
// screen, so any table gets click-to-sort for free.
type Sort struct {
	Col   int
	Asc   bool
	Hover int
}

// Toggle activates column col, flipping direction if it's already active; a new
// column starts in ascDefault direction (text columns usually want A→Z).
func (s *Sort) Toggle(col, count int, ascDefault bool) {
	if col < 0 || col >= count {
		return
	}
	if col == s.Col {
		s.Asc = !s.Asc
		return
	}
	s.Col, s.Asc = col, ascDefault
}

// ColAt maps an x (with the columns starting at startX) to a column index, or -1.
func ColAt(cols []Col, startX, x int) int {
	pos := startX
	for i, c := range cols {
		if x >= pos && x < pos+c.Width {
			return i
		}
		pos += c.Width + TableGapW
	}
	return -1
}

// SortHeader renders the header with a ▲/▼ on the active column (clay); the
// column under the mouse brightens to cream to signal it's clickable.
func SortHeader(cols []Col, s Sort) string {
	parts := make([]string, len(cols))
	for i, c := range cols {
		title := c.Title
		st := Dim
		if i == s.Col {
			if s.Asc {
				title += " ▲"
			} else {
				title += " ▼"
			}
			st = Clay
		} else if i == s.Hover {
			st = Cream
		}
		parts[i] = st.Render(colAlign(title, c.Width, c.Right))
	}
	return strings.Join(parts, tableGap)
}

// SortRows returns a copy of rows ordered by the active column's Cell.Key.
func SortRows(rows [][]Cell, s Sort) [][]Cell {
	out := make([][]Cell, len(rows))
	copy(out, rows)
	key := func(r []Cell) SortKey {
		if s.Col >= 0 && s.Col < len(r) {
			return r[s.Col].Key
		}
		return SortKey{}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := key(out[i]), key(out[j])
		if s.Asc {
			return a.less(b)
		}
		return b.less(a)
	})
	return out
}

// colAlign pads (or truncates) s to exactly w visible cells, left- or
// right-justified. Visible-width aware, so multibyte glyphs (sort arrows, …)
// stay aligned.
func colAlign(s string, w int, right bool) string {
	if lipgloss.Width(s) > w {
		s = ansi.Truncate(s, w, "…")
	}
	gap := w - lipgloss.Width(s)
	if gap <= 0 {
		return s
	}
	if right {
		return strings.Repeat(" ", gap) + s
	}
	return s + strings.Repeat(" ", gap)
}

// TableHeader renders the dim, aligned column header row.
func TableHeader(cols []Col) string {
	parts := make([]string, len(cols))
	for i, c := range cols {
		parts[i] = Dim.Render(colAlign(c.Title, c.Width, c.Right))
	}
	return strings.Join(parts, tableGap)
}

// TableHeaderStyled renders the header with a per-column style (styles must be
// the same length as cols) — used where some headers are active, e.g. the
// sorted column or the one under the mouse.
func TableHeaderStyled(cols []Col, styles []lipgloss.Style) string {
	parts := make([]string, len(cols))
	for i, c := range cols {
		parts[i] = styles[i].Render(colAlign(c.Title, c.Width, c.Right))
	}
	return strings.Join(parts, tableGap)
}

// TableRow renders one row's styled cells, aligned to the columns.
func TableRow(cols []Col, cells []Cell) string {
	parts := make([]string, len(cols))
	for i, c := range cols {
		var cell Cell
		if i < len(cells) {
			cell = cells[i]
		}
		parts[i] = cell.Style.Render(colAlign(cell.Text, c.Width, c.Right))
	}
	return strings.Join(parts, tableGap)
}

// TableRowBar renders a row as a full-width selected highlight bar: plain text
// (cell styling dropped so the background fills), padded to width and a leading
// space so it lines up under a 1-col gutter.
func TableRowBar(cols []Col, cells []Cell, width int) string {
	parts := make([]string, len(cols))
	for i, c := range cols {
		text := ""
		if i < len(cells) {
			text = cells[i].Text
		}
		parts[i] = colAlign(text, c.Width, c.Right)
	}
	return Bar.Render(AnsiPad(" "+strings.Join(parts, tableGap), width))
}

// TableWindow returns the row offset that keeps cursor visible in a viewport of
// h rows — shared scroll math for any windowed list.
func TableWindow(total, h, cursor int) int {
	if h <= 0 || total <= h || cursor < h {
		return 0
	}
	off := cursor - h + 1
	if maxOff := total - h; off > maxOff {
		off = maxOff
	}
	if off < 0 {
		off = 0
	}
	return off
}
