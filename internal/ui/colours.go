package ui

import (
	"fmt"
	"math"

	"github.com/nomad-software/qute/qtgui"
)

// qute's QColor is a plain value of 8-bit channels, so what Qt's QColor
// would work out of a colour is worked out here.

// parseColour returns the colour as the settings write it, #rrggbb or
// #aarrggbb, which they check that it is.
func parseColour(colour string) qtgui.QColor {
	c, err := qtgui.ParseColor(colour)
	if err != nil {
		// Black, as Qt draws a colour it cannot read. qute's zero QColor
		// would be clear instead.
		return qtgui.QColor{A: 255}
	}
	return c
}

// withAlpha returns the colour with the opacity, from 0 to 1, as Qt's
// QColor.setAlphaF has it.
func withAlpha(c qtgui.QColor, alpha float64) qtgui.QColor {
	c.A = uint8(math.Round(min(max(alpha, 0), 1) * 255))
	return c
}

// lightness returns the lightness of the colour, from 0 to 255, as Qt's
// QColor.lightness works it out: half the largest channel and the smallest,
// in 16 bits, then rounded down to 8.
func lightness(c qtgui.QColor) int {
	most := max(c.R, c.G, c.B)
	least := min(c.R, c.G, c.B)
	l := int(math.Round((float64(most) + float64(least)) / 510 * 65535))
	return (l - (l >> 8) + 0x80) >> 8
}

// readableOn returns black or white, whichever can be read on the colour.
func readableOn(colour qtgui.QColor) string {
	if lightness(colour) > 140 {
		return "#000000"
	}
	return "#ffffff"
}

// colourName writes a colour as the settings do: #rrggbb where it is opaque,
// and #aarrggbb where it is not.
func colourName(c qtgui.QColor) string {
	if c.A == 255 {
		return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
	}
	return fmt.Sprintf("#%02x%02x%02x%02x", c.A, c.R, c.G, c.B)
}
