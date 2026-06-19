// Package diff renders a colored, context-collapsed line diff between two files.
package diff

import (
	"fmt"
	"strings"

	"clihome/internal/img"
	"clihome/internal/ui"
)

func isBinary(b []byte) bool {
	n := min(len(b), 8000)
	for i := 0; i < n; i++ {
		if b[i] == 0 {
			return true
		}
	}
	return false
}

// lineDiff returns rows of [type, text] where type is " " | "-" | "+",
// or nil if the inputs are too large to diff cheaply.
func lineDiff(a, b []string) [][2]string {
	n, m := len(a), len(b)
	if n*m > 4_000_000 {
		return nil
	}
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else {
				dp[i][j] = max(dp[i+1][j], dp[i][j+1])
			}
		}
	}
	var rows [][2]string
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			rows = append(rows, [2]string{" ", a[i]})
			i, j = i+1, j+1
		case dp[i+1][j] >= dp[i][j+1]:
			rows = append(rows, [2]string{"-", a[i]})
			i++
		default:
			rows = append(rows, [2]string{"+", b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		rows = append(rows, [2]string{"-", a[i]})
	}
	for ; j < m; j++ {
		rows = append(rows, [2]string{"+", b[j]})
	}
	return rows
}

// Format renders a colored diff of two files (old -> new), collapsing unchanged
// regions to a few lines of context. width is the drawable column width of the
// pane it will be shown in — used to size inline image previews.
func Format(oldB, newB []byte, width int) string {
	if art, ok := imageDiff(oldB, newB, width); ok {
		return art
	}
	if isBinary(oldB) || isBinary(newB) {
		return ui.Dim.Render(fmt.Sprintf("    (binary — %d → %d bytes)", len(oldB), len(newB)))
	}
	var a, b []string
	if len(oldB) > 0 {
		a = strings.Split(string(oldB), "\n")
	}
	if len(newB) > 0 {
		b = strings.Split(string(newB), "\n")
	}
	rows := lineDiff(a, b)
	if rows == nil {
		return ui.Dim.Render(fmt.Sprintf("    (large file — %d → %d lines; diff skipped)", len(a), len(b)))
	}

	const ctx = 3
	show := make([]bool, len(rows))
	for idx, r := range rows {
		if r[0] != " " {
			for k := max(0, idx-ctx); k <= min(len(rows)-1, idx+ctx); k++ {
				show[k] = true
			}
		}
	}
	var out []string
	gap := false
	for idx, r := range rows {
		if !show[idx] {
			if !gap {
				out = append(out, ui.Dim.Render("    ⋯"))
				gap = true
			}
			continue
		}
		gap = false
		switch r[0] {
		case "+":
			out = append(out, ui.Green.Render("  + "+r[1]))
		case "-":
			out = append(out, ui.Red.Render("  - "+r[1]))
		default:
			out = append(out, ui.Dim.Render("    "+r[1]))
		}
	}
	if len(out) == 0 {
		return ui.Dim.Render("    (no textual changes)")
	}
	return strings.Join(out, "\n")
}

// imgMaxRows caps an inline preview's height; the diff pane scrolls, but a hard
// cap keeps rendering cheap for huge images.
const imgMaxRows = 28

// imageDiff renders an inline preview when either side is a decodable image: the
// resulting (new) picture for adds/changes, or the old picture for removals,
// captioned with the byte sizes. Returns ("", false) when neither side is an image.
func imageDiff(oldB, newB []byte, width int) (string, bool) {
	newImg := img.Decodable(newB)
	oldImg := img.Decodable(oldB)
	if !newImg && !oldImg {
		return "", false
	}
	w := max(width, 8)
	var caption, art string
	switch {
	case newImg && oldImg:
		caption = ui.Dim.Render(fmt.Sprintf("    image · %d → %d bytes", len(oldB), len(newB)))
		art, _ = img.Render(newB, w, imgMaxRows)
	case newImg:
		caption = ui.Green.Render(fmt.Sprintf("  + image · %d bytes", len(newB)))
		art, _ = img.Render(newB, w, imgMaxRows)
	default: // only the old side is an image → it's being removed
		caption = ui.Red.Render(fmt.Sprintf("  − image · %d bytes (removed)", len(oldB)))
		art, _ = img.Render(oldB, w, imgMaxRows)
	}
	if art == "" {
		return caption, true
	}
	return caption + "\n\n" + art, true
}
