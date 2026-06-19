// Package ui holds shared Lip Gloss styles and small layout helpers used by both
// the one-shot commands and the Bubble Tea interface.
package ui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"clihome/internal/home"
)

// Palette — keyed to the logo's warm clay/coral.
var (
	Clay   = lipgloss.NewStyle().Foreground(lipgloss.Color("#D97757"))
	Cream  = lipgloss.NewStyle().Foreground(lipgloss.Color("#E1D5C4"))
	Cyan   = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	Blue   = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
	Green  = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	Yellow = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	Red    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	Dim    = lipgloss.NewStyle().Faint(true)
	Bold   = lipgloss.NewStyle().Bold(true)
	// Bar is the selected-row highlight (clay background, near-black text).
	Bar = lipgloss.NewStyle().Background(lipgloss.Color("#D97757")).Foreground(lipgloss.Color("#1C1613")).Bold(true)
)

// SetAccent re-themes the app's accent (the clay highlight) to hex — affects the
// logo, headers, cursors, selection bar, and the Claude tint everywhere.
func SetAccent(hex string) {
	Clay = lipgloss.NewStyle().Foreground(lipgloss.Color(hex))
	Bar = lipgloss.NewStyle().Background(lipgloss.Color(hex)).Foreground(lipgloss.Color("#1C1613")).Bold(true)
}

// Banner renders the slim brand header.
func Banner(sub string) string {
	if sub == "" {
		sub = "AI CLI config home manager"
	}
	head := "  " + Clay.Render("◆") + " " + Clay.Bold(true).Render("clihome") + "  " + Dim.Render("·") + "  " + Dim.Render(sub)
	return "\n" + head + "\n  " + Dim.Render(strings.Repeat("─", 52)) + "\n"
}

// Pad right-pads s to width w (rune-aware).
func Pad(s string, w int) string {
	if n := utf8.RuneCountInString(s); n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return s
}

// OrDash returns "—" for an empty string.
func OrDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// RelTime renders a human "time ago", or "—" for the zero time.
func RelTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	s := time.Since(t).Seconds()
	switch {
	case s < 60:
		return "just now"
	case s < 3600:
		return fmt.Sprintf("%dm ago", int(s/60))
	case s < 86400:
		return fmt.Sprintf("%dh ago", int(s/3600))
	case s < 7*86400:
		return fmt.Sprintf("%dd ago", int(s/86400))
	}
	return t.Format("Jan 2")
}

// RowData holds the display cells for one home.
type RowData struct {
	Name, Tool, Account, Plan, Model, Last string
	HasAcct, Codex                         bool
}

// Widths holds the computed table column widths.
type Widths struct{ Name, Tool, Account, Plan, Model int }

// Rows computes table cells + column widths for the given homes.
func Rows(homes []home.Home) ([]RowData, Widths) {
	w := Widths{Name: 4, Tool: 4, Account: 7, Plan: 4, Model: 5}
	rows := make([]RowData, 0, len(homes))
	for _, h := range homes {
		email := h.Provider.Email(h.Dir)
		r := RowData{
			Name:    h.Name,
			Tool:    h.Provider.Label,
			Account: OrDash(email),
			Plan:    OrDash(h.Provider.Plan(h.Dir)),
			Model:   OrDash(h.Provider.Model(h.Dir)),
			Last:    RelTime(home.LastActive(h)),
			HasAcct: email != "",
			Codex:   h.Provider.ID == "codex",
		}
		rows = append(rows, r)
		w.Name = max(w.Name, len(r.Name))
		w.Tool = max(w.Tool, len(r.Tool))
		w.Account = max(w.Account, len(r.Account))
		w.Plan = max(w.Plan, len(r.Plan))
		w.Model = max(w.Model, len(r.Model))
	}
	return rows, w
}

// Tint returns the provider tint (clay for Claude, blue for Codex).
func Tint(codex bool) lipgloss.Style {
	if codex {
		return Blue
	}
	return Clay
}
