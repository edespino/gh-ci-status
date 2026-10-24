// SPDX-License-Identifier: Apache-2.0

// Package banner draws the ci-status logo: the wordmark in box-drawing
// letters, coloured from red to green, for terminals, web pages and the
// README.
package banner

import (
	"fmt"
	"strings"
)

// Wordmark is "ci-status" in box-drawing letters; the terminal banner, the
// pages' logo and the README image (assets/banner.svg) are drawn from it.
var Wordmark = []string{
	"┌─┐┬   ┌─┐┌┬┐┌─┐┌┬┐┬ ┬┌─┐",
	"│  │ ─ └─┐ │ ├─┤ │ │ │└─┐",
	"└─┘┴   └─┘ ┴ ┴ ┴ ┴ └─┘└─┘",
}

// Tagline is printed under the wordmark.
const Tagline = "from red to green"

// gradient runs from red through amber to green in xterm 256-colour codes,
// deep enough to read on light and dark terminals.
var gradient = []int{160, 166, 172, 136, 100, 64, 28}

// Terminal is the banner for a terminal: the wordmark, tinted from red on the
// left to green on the right when color is set, and the tagline.
func Terminal(color bool) string {
	var b strings.Builder
	for _, line := range Wordmark {
		cols := []rune(line)
		b.WriteString(" ")
		last := -1
		for i, r := range cols {
			if c := gradient[i*(len(gradient)-1)/(len(cols)-1)]; color && r != ' ' && c != last {
				fmt.Fprintf(&b, "\x1b[38;5;%dm", c)
				last = c
			}
			b.WriteRune(r)
		}
		if color {
			b.WriteString("\x1b[0m")
		}
		b.WriteString("\n")
	}
	if color {
		return b.String() + " \x1b[2m" + Tagline + "\x1b[0m\n"
	}
	return b.String() + " " + Tagline + "\n"
}

// UseColor follows the NO_COLOR convention (no-color.org) and plain
// terminals (TERM=dumb).
func UseColor(getenv func(string) string) bool {
	return getenv("NO_COLOR") == "" && getenv("TERM") != "dumb"
}

// SVG draws the wordmark's box-drawing characters as strokes,
// coloured red to green. Tagged, it is the README image (assets/banner.svg)
// with the tagline below; untagged, it is the pages' header mark, at one fixed
// size so every page shows the same logo.
func SVG(tagged bool) string {
	const w, h = 14.0, 22.0
	pad, stroke := 3.0, 5.0 // the page mark is drawn small, so heavier
	if tagged {
		pad, stroke = 12, 3
	}
	edges := map[rune]string{
		'─': "lr", '│': "ud", '┌': "rd", '┐': "ld", '└': "ur", '┘': "ul",
		'┬': "lrd", '┴': "lru", '├': "udr", '┤': "udl",
	}
	cols := len([]rune(Wordmark[0]))
	width := 2*pad + float64(cols)*w
	height := 2*pad + float64(len(Wordmark))*h
	var d strings.Builder
	for row, line := range Wordmark {
		for col, r := range []rune(line) {
			x0, y0 := pad+float64(col)*w, pad+float64(row)*h
			cx, cy := x0+w/2, y0+h/2
			for _, e := range edges[r] {
				x, y := cx, cy
				switch e {
				case 'l':
					x = x0
				case 'r':
					x = x0 + w
				case 'u':
					y = y0
				case 'd':
					y = y0 + h
				}
				fmt.Fprintf(&d, "M%g %gL%g %g", cx, cy, x, y)
			}
		}
	}
	const markHeight = 22.0
	open, label, text := fmt.Sprintf(`<svg class="wordmark" xmlns="http://www.w3.org/2000/svg" width="%.0f" height="%g" viewBox="0 0 %g %g" role="img" aria-label="ci-status">`,
		markHeight*width/height, markHeight, width, height), "", ""
	if tagged {
		height += 34
		label = "ci-status: " + Tagline
		open = fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%g" height="%g" viewBox="0 0 %g %g" role="img" aria-label="%s">`, width, height, width, height, label)
		text = fmt.Sprintf(`<text x="%g" y="%g" font-family="ui-monospace,SFMono-Regular,Menlo,Consolas,monospace" font-size="16" fill="#71717a">%s</text>`+"\n",
			pad, pad+float64(len(Wordmark))*h+26, Tagline)
	}
	return open + "\n" + fmt.Sprintf(`<defs><linearGradient id="ci-status-red-to-green" gradientUnits="userSpaceOnUse" x1="%g" y1="0" x2="%g" y2="0">
<stop offset="0" stop-color="#dc2626"/><stop offset="0.5" stop-color="#ca8a04"/><stop offset="1" stop-color="#16a34a"/>
</linearGradient></defs>
<path d="%s" fill="none" stroke="url(#ci-status-red-to-green)" stroke-width="%g" stroke-linecap="square"/>
`, pad, width-pad, d.String(), stroke) + text + "</svg>\n"
}
