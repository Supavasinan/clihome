// Package img renders raster images (PNG/JPEG/GIF) as half-block ANSI art so the
// TUI can preview pictures inline — no Sixel/Kitty/iTerm protocol needed, just
// truecolor, which clihome already forces. Each character cell ("▀") stacks two
// vertical pixels: the foreground paints the top pixel, the background the
// bottom one, doubling vertical resolution and keeping cells roughly square.
package img

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"  // register GIF decoder
	_ "image/jpeg" // register JPEG decoder
	_ "image/png"  // register PNG decoder
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// upperHalf is the glyph whose top half is the foreground and bottom the
// background — the building block of the renderer.
const upperHalf = "▀"

// bg is the app's near-black surface; transparent pixels composite onto it so
// PNGs with alpha don't show as harsh black.
var bg = [3]uint32{0x1c, 0x16, 0x13}

// Decodable reports whether b decodes as a supported raster image.
func Decodable(b []byte) bool {
	_, _, err := image.Decode(bytes.NewReader(b))
	return err == nil
}

// Render turns image bytes into half-block ANSI art bounded by maxW cells wide
// and maxH cell-rows tall, preserving aspect ratio. It returns ("", false) when
// b is not a decodable image, so callers can fall back to a text summary.
func Render(b []byte, maxW, maxH int) (string, bool) {
	src, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return "", false
	}
	if maxW < 1 {
		maxW = 1
	}
	if maxH < 1 {
		maxH = 1
	}

	bnd := src.Bounds()
	sw, sh := bnd.Dx(), bnd.Dy()
	if sw <= 0 || sh <= 0 {
		return "", false
	}

	// A cell is 1 px wide and 2 px tall, so the sampled grid is cols × (rows*2)
	// pixels. Fit it inside the cell budget while keeping the source aspect.
	cols := maxW
	rows := max(cols*sh/(sw*2), 1)
	if rows > maxH {
		rows = maxH
		cols = min(max(rows*2*sw/sh, 1), maxW)
	}

	// sample returns the composited 8-bit RGB of the source pixel nearest to the
	// (col, pixel-row) position in the cols × (rows*2) target grid.
	sample := func(c, pr int) (uint8, uint8, uint8) {
		sx := bnd.Min.X + c*sw/cols
		sy := bnd.Min.Y + pr*sh/(rows*2)
		r, g, bl, a := src.At(sx, sy).RGBA() // 16-bit, alpha-premultiplied
		inv := 0xffff - a
		r = (r + bg[0]*257*inv/0xffff) >> 8
		g = (g + bg[1]*257*inv/0xffff) >> 8
		bl = (bl + bg[2]*257*inv/0xffff) >> 8
		return uint8(r), uint8(g), uint8(bl)
	}

	hex := func(r, g, b uint8) lipgloss.Color {
		return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", r, g, b))
	}

	var out strings.Builder
	for row := 0; row < rows; row++ {
		for c := 0; c < cols; c++ {
			tr, tg, tb := sample(c, row*2)    // top pixel  → foreground
			br, bgc, bb := sample(c, row*2+1) // bottom pixel → background
			out.WriteString(lipgloss.NewStyle().
				Foreground(hex(tr, tg, tb)).
				Background(hex(br, bgc, bb)).
				Render(upperHalf))
		}
		if row < rows-1 {
			out.WriteByte('\n')
		}
	}
	return out.String(), true
}
