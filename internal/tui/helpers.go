package tui

import (
	"os"
	"strconv"
	"strings"

	"clihome/internal/plan"
	"clihome/internal/ui"
)

// stamp renders a 20060102-150405 folder name as "2006-01-02 15:04".
func stamp(s string) string {
	if len(s) != 15 || s[8] != '-' {
		return s
	}
	return s[:4] + "-" + s[4:6] + "-" + s[6:8] + " " + s[9:11] + ":" + s[11:13]
}

// shortPath rewrites an absolute path under $HOME to a ~-prefixed one.
func shortPath(p string) string {
	if h, err := os.UserHomeDir(); err == nil {
		return strings.Replace(p, h, "~", 1)
	}
	return p
}

// entrySummary renders an entry's per-file change counts (+new ~changed -removed).
func entrySummary(e plan.Entry) string {
	var p []string
	if e.Nw > 0 {
		p = append(p, ui.Green.Render("+"+strconv.Itoa(e.Nw)))
	}
	if e.Ch > 0 {
		p = append(p, ui.Yellow.Render("~"+strconv.Itoa(e.Ch)))
	}
	if e.Rm > 0 {
		p = append(p, ui.Red.Render("-"+strconv.Itoa(e.Rm)))
	}
	return strings.Join(p, " ")
}

// stateBadge renders a home's sync state as a colored badge.
func stateBadge(s string) string {
	switch s {
	case "drift":
		return ui.Yellow.Render("drift")
	case "clean":
		return ui.Green.Render("clean")
	case "base":
		return ui.Clay.Render("base")
	default:
		return ui.Dim.Render(s)
	}
}

// readForDiff reads a file for diffing, rendering symlinks as "→ target".
func readForDiff(p string) []byte {
	fi, err := os.Lstat(p)
	if err != nil {
		return nil
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t, _ := os.Readlink(p)
		return []byte("→ " + t + "\n")
	}
	d, _ := os.ReadFile(p)
	return d
}

func fileExists(p string) bool { _, err := os.Lstat(p); return err == nil }
