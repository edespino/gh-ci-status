// SPDX-License-Identifier: Apache-2.0

// Package banner draws the gh-ci-status logo: the name in box-drawing
// letters, "gh-" in grey and "ci-status" coloured from red to green, for
// terminals, web pages and the README.
package banner

import (
	"fmt"
	"math"
	"strings"
)

// Wordmark is "gh-ci-status" in box-drawing letters; the terminal banner, the
// pages' logo and the README image (assets/banner.svg) are drawn from it.
var Wordmark = []string{
	"┌─┐┬ ┬   ┌─┐┬   ┌─┐┌┬┐┌─┐┌┬┐┬ ┬┌─┐",
	"│ ┬├─┤ ─ │  │ ─ └─┐ │ ├─┤ │ │ │└─┐",
	"└─┘┴ ┴   └─┘┴   └─┘ ┴ ┴ ┴ ┴ └─┘└─┘",
}

// prefix is how many columns of the wordmark spell "gh-", drawn in grey; the
// rest spells "ci-status", drawn from red to green.
const prefix = 9

// Tagline is printed under the wordmark.
const Tagline = "from red to green"

// grey is "gh-" in xterm 256-colour code; gradient runs from red through amber
// to green. Both are deep enough to read on light and dark terminals.
var (
	grey     = 244
	gradient = []int{160, 166, 172, 136, 100, 64, 28}
)

// Terminal is the banner for a terminal: the wordmark, with "gh-" grey and
// "ci-status" tinted from red to green when color is set, and the tagline.
func Terminal(color bool) string {
	var b strings.Builder
	for _, line := range Wordmark {
		cols := []rune(line)
		b.WriteString(" ")
		last := -1
		for i, r := range cols {
			c := grey
			if i >= prefix {
				c = gradient[(i-prefix)*(len(gradient)-1)/(len(cols)-prefix-1)]
			}
			if color && r != ' ' && c != last {
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

// SVG draws the wordmark's box-drawing characters as strokes: "gh-" in grey
// and "ci-status" from red to green. Tagged, it is the README image
// (assets/banner.svg) with the tagline below and a fixed grey that reads on
// light and dark backgrounds; untagged, it is the pages' logo at one fixed
// size, its grey taken from the page's text colour (currentColor).
func SVG(tagged bool) string {
	const w, h = 14.0, 22.0
	pad, stroke, greyStroke := 3.0, 5.0, "currentColor" // the page logo is drawn small, so heavier
	if tagged {
		pad, stroke, greyStroke = 12, 3, "#888888"
	}
	cols := len([]rune(Wordmark[0]))
	width := 2*pad + float64(cols)*w
	height := 2*pad + float64(len(Wordmark))*h
	gh, rest := strokes(0, prefix, pad, pad, w, h), strokes(prefix, cols, pad+prefix*w, pad, w, h)
	const markHeight = 22.0
	open := fmt.Sprintf(`<svg class="wordmark" xmlns="http://www.w3.org/2000/svg" width="%.0f" height="%g" viewBox="0 0 %g %g" role="img" aria-label="gh-ci-status">`,
		markHeight*width/height, markHeight, width, height)
	text := ""
	if tagged {
		height += 34
		open = fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%g" height="%g" viewBox="0 0 %g %g" role="img" aria-label="gh-ci-status: %s">`,
			width, height, width, height, Tagline)
		text = fmt.Sprintf(`<text x="%g" y="%g" font-family="ui-monospace,SFMono-Regular,Menlo,Consolas,monospace" font-size="16" fill="#71717a">%s</text>`+"\n",
			pad, pad+float64(len(Wordmark))*h+26, Tagline)
	}
	return open + "\n" + fmt.Sprintf(`<defs><linearGradient id="ci-status-red-to-green" gradientUnits="userSpaceOnUse" x1="%g" y1="0" x2="%g" y2="0">
<stop offset="0" stop-color="#dc2626"/><stop offset="0.5" stop-color="#ca8a04"/><stop offset="1" stop-color="#16a34a"/>
</linearGradient></defs>
<path d="%s" fill="none" stroke="%s" stroke-width="%g" stroke-linecap="square"/>
<path d="%s" fill="none" stroke="url(#ci-status-red-to-green)" stroke-width="%g" stroke-linecap="square"/>
`, pad+prefix*w, width-pad, gh.d, greyStroke, stroke, rest.d, stroke) + text + "</svg>\n"
}

// drawing is SVG path data and the box its lines span.
type drawing struct {
	d                      string
	minX, minY, maxX, maxY float64
}

// strokes draws the wordmark's columns from to to (exclusive) as lines, in
// cells of w by h, the first column's left edge at x0 and the top at y0.
func strokes(from, to int, x0, y0, w, h float64) drawing {
	edges := map[rune]string{
		'─': "lr", '│': "ud", '┌': "rd", '┐': "ld", '└': "ur", '┘': "ul",
		'┬': "lrd", '┴': "lru", '├': "udr", '┤': "udl",
	}
	dr := drawing{minX: math.Inf(1), minY: math.Inf(1), maxX: math.Inf(-1), maxY: math.Inf(-1)}
	var b strings.Builder
	for row, line := range Wordmark {
		runes := []rune(line)
		for col := from; col < to; col++ {
			left, top := x0+float64(col-from)*w, y0+float64(row)*h
			cx, cy := left+w/2, top+h/2
			for _, e := range edges[runes[col]] {
				x, y := cx, cy
				switch e {
				case 'l':
					x = left
				case 'r':
					x = left + w
				case 'u':
					y = top
				case 'd':
					y = top + h
				}
				fmt.Fprintf(&b, "M%g %gL%g %g", cx, cy, x, y)
				dr.minX, dr.maxX = math.Min(dr.minX, math.Min(cx, x)), math.Max(dr.maxX, math.Max(cx, x))
				dr.minY, dr.maxY = math.Min(dr.minY, math.Min(cy, y)), math.Max(dr.maxY, math.Max(cy, y))
			}
		}
	}
	dr.d = b.String()
	return dr
}

// Icon is a square image for places that want one, such as a Slack app
// (assets/icon.svg, and assets/icon.png made from it): "ci" from the
// wordmark, red to green, centred on a dark square of 512 by 512.
func Icon() string {
	const size, letters = 512.0, 310.0 // the letters' larger side, in pixels
	ci := strokes(prefix, prefix+4, 0, 0, 14, 22)
	k := letters / math.Max(ci.maxX-ci.minX, ci.maxY-ci.minY)
	tx := size/2 - (ci.minX+ci.maxX)/2*k
	ty := size/2 - (ci.minY+ci.maxY)/2*k
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="512" height="512" viewBox="0 0 512 512" role="img" aria-label="gh-ci-status">
<defs><linearGradient id="ci-status-red-to-green" gradientUnits="userSpaceOnUse" x1="%g" y1="0" x2="%g" y2="0">
<stop offset="0" stop-color="#dc2626"/><stop offset="0.5" stop-color="#ca8a04"/><stop offset="1" stop-color="#16a34a"/>
</linearGradient></defs>
<rect width="512" height="512" fill="#18181b"/>
<g transform="translate(%.2f %.2f) scale(%.4f)"><path d="%s" fill="none" stroke="url(#ci-status-red-to-green)" stroke-width="5" stroke-linecap="square"/></g>
</svg>
`, ci.minX, ci.maxX, tx, ty, k, ci.d)
}
