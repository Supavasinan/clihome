// Package ui — reusable rendering primitives shared across every screen.
//
// These were previously duplicated inside the Bubble Tea layer; centralizing
// them here keeps each screen file small and the look consistent everywhere.
package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Border is the standard rounded pane border used by every panel.
var Border = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("238"))

// BarDanger is the selected-row highlight for a destructive action — a muted red
// background, mirroring Bar's role for normal rows.
var BarDanger = lipgloss.NewStyle().Background(lipgloss.Color("#a23a2b")).Foreground(lipgloss.Color("#f3e3dc")).Bold(true)

// Strip removes ANSI styling from s, leaving plain text — used when placing text
// onto a background highlight, where inner color resets would break the fill.
func Strip(s string) string { return ansi.Strip(s) }

// Pane renders a titled rounded box of inner height h. The body is clipped to fit
// (title + blank line + h-2 content rows) so the pane NEVER grows past h: an
// expanded pane would shove everything below it down and silently break the fixed
// row offsets the mouse hit-tests depend on. Callers that must show all their
// content size the pane to it (or window/scroll it) before calling.
func Pane(title, content string, w, h int) string {
	body := strings.TrimRight(content, "\n")
	if rows := strings.Split(body, "\n"); len(rows) > h-2 {
		body = strings.Join(rows[:max(h-2, 0)], "\n")
	}
	inner := Dim.Render(strings.ToUpper(title)) + "\n\n" + body
	return Border.Width(w).Height(h).Padding(0, 1).Render(inner)
}

// Centered horizontally centers a (possibly colored) line across width w.
func Centered(w int, s string) string {
	return lipgloss.NewStyle().Width(w).Align(lipgloss.Center).Render(s)
}

// Screen is the standard full-screen frame every view shares: a top header, the
// body, and a footer, separated by the app's fixed vertical rhythm. Centralizing
// it keeps the spacing identical across screens and out of each view function.
func Screen(header, body, footer string) string {
	return "\n" + header + "\n\n" + body + "\n\n" + footer
}

// FitHeight caps s to at most h lines, dropping any overflow from the bottom. On
// the alt screen, a frame taller than the terminal scrolls its top off — which
// silently shifts every fixed row offset the mouse hit-tests rely on, so hover
// and clicks land on the wrong row. Clamping keeps the frame's top anchored at
// terminal row 0 so those offsets stay valid at any window size.
func FitHeight(s string, h int) string {
	if h < 1 {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= h {
		return s
	}
	return strings.Join(lines[:h], "\n")
}

// Pointer is the selection caret shown before the highlighted row in list views.
const Pointer = "❯ "

// HoverMark is the slim bar that flags a hovered (not-yet-selected) row.
const HoverMark = "▌"

// Cursor returns a list row's leading gutter: the clay caret when selected, else
// two blank cells so selected and unselected rows stay aligned. Centralizes the
// caret glyph + accent that every list screen would otherwise hard-code.
func Cursor(selected bool) string { return CursorState(selected, false) }

// CursorState renders a list row's leading caret for its interaction state: the
// clay caret when selected, a cream caret hint when merely hovered, else a blank
// two-cell gutter. Keeps the selected/hover affordance identical across screens.
func CursorState(selected, hovered bool) string {
	switch {
	case selected:
		return Clay.Render(Pointer)
	case hovered:
		return Cream.Render(Pointer)
	default:
		return "  "
	}
}

// HoverGutter is the two-cell left gutter for a hovered table row (a cream bar +
// space) — the table-row counterpart to CursorState's caret, aligning with the
// blank "  " gutter of resting rows.
func HoverGutter() string { return Cream.Render(HoverMark) + " " }

// LevelColor maps a remaining-capacity percent (0–100) to a status color: green
// while plenty remains, amber when low (<50%), red when nearly exhausted (<20%).
func LevelColor(pct float64) lipgloss.Style {
	switch {
	case pct < 20:
		return Red
	case pct < 50:
		return Yellow
	default:
		return Green
	}
}

// Gauge renders a horizontal bar of width cells filled to pct% (0–100): a solid
// run tinted by LevelColor over a dim empty track. The fill is clamped so an
// out-of-range percent can't overflow or underflow the bar.
func Gauge(pct float64, width int) string {
	if width < 0 {
		width = 0
	}
	fill := min(max(int(pct/100*float64(width)+0.5), 0), width)
	return LevelColor(pct).Render(strings.Repeat("█", fill)) + Dim.Render(strings.Repeat("░", width-fill))
}

// ClampScroll bounds a desired scroll offset to [0, max(total-height, 0)] — the
// shared scroll math keeping any windowed pane's last page full.
func ClampScroll(v, total, height int) int {
	maxOff := max(total-height, 0)
	if v < 0 {
		return 0
	}
	if v > maxOff {
		return maxOff
	}
	return v
}

// ScrollView renders height rows from lines, each padded/truncated to width, with
// a Scrollbar column appended on the right. scroll is the desired top offset,
// clamped so the final page stays full. The block carries no trailing newline, so
// it drops straight into a Pane.
func ScrollView(lines []string, width, height, scroll int) string {
	if height < 0 {
		height = 0
	}
	offset := max(min(scroll, len(lines)-height), 0)
	bar := Scrollbar(height, len(lines), offset)
	var b strings.Builder
	for i := range height {
		line := ""
		if idx := offset + i; idx >= 0 && idx < len(lines) {
			line = lines[idx]
		}
		b.WriteString(AnsiPad(line, width) + bar[i])
		if i < height-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// Scrollbar returns h lines: a dim track with a clay thumb at the scroll offset.
func Scrollbar(h, total, offset int) []string {
	col := make([]string, h)
	if total <= h || h <= 0 {
		for i := range col {
			col[i] = " "
		}
		return col
	}
	thumb := max(1, h*h/total)
	pos := 0
	if maxOff := total - h; maxOff > 0 {
		pos = offset * (h - thumb) / maxOff
	}
	for i := range col {
		if i >= pos && i < pos+thumb {
			col[i] = Clay.Render("█")
		} else {
			col[i] = Dim.Render("░")
		}
	}
	return col
}

// AnsiPad truncates/pads an (ANSI-colored) string to exactly w visible cells.
func AnsiPad(s string, w int) string {
	if w < 0 {
		w = 0
	}
	if lipgloss.Width(s) > w {
		s = ansi.Truncate(s, w, "…")
	}
	if gap := w - lipgloss.Width(s); gap > 0 {
		s += strings.Repeat(" ", gap)
	}
	return s
}

// Truncate keeps the tail of s within n runes, prefixing "…" when clipped — used
// for paths, where the end (filename) matters more than the leading dirs.
func Truncate(s string, n int) string {
	if n < 1 {
		n = 1
	}
	if len(s) > n {
		return "…" + s[len(s)-(n-1):]
	}
	return s
}

// RightAlign right-justifies s within width w.
func RightAlign(s string, w int) string {
	if len(s) < w {
		return strings.Repeat(" ", w-len(s)) + s
	}
	return s
}

// Plural renders "1 file" / "3 files".
func Plural(n int, w string) string {
	if n == 1 {
		return "1 " + w
	}
	return fmt.Sprintf("%d %ss", n, w)
}

// FmtTokens renders a token count compactly (1.9B / 12.3M / 4.5K / 123).
func FmtTokens(n int64) string {
	switch {
	case n >= 1e9:
		return fmt.Sprintf("%.1fB", float64(n)/1e9)
	case n >= 1e6:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1e3:
		return fmt.Sprintf("%.1fK", float64(n)/1e3)
	}
	return strconv.FormatInt(n, 10)
}

// FmtCost renders an estimated dollar cost ("~$4.56", thousands-grouped above $100).
func FmtCost(c float64) string {
	if c < 100 {
		return fmt.Sprintf("~$%.2f", c)
	}
	s := strconv.FormatInt(int64(c+0.5), 10)
	var out []byte
	for i := range len(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, s[i])
	}
	return "~$" + string(out)
}
