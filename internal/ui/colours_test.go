package ui

import (
	"testing"

	"github.com/nomad-software/qute/qtgui"
)

func TestParseColour(t *testing.T) {
	for _, c := range []struct {
		in   string
		want qtgui.QColor
	}{
		{"#268bd2", qtgui.QColor{R: 0x26, G: 0x8b, B: 0xd2, A: 255}},
		{"#66aea79f", qtgui.QColor{R: 0xae, G: 0xa7, B: 0x9f, A: 0x66}},
		// What cannot be read is black, as Qt draws it, not clear.
		{"", qtgui.QColor{A: 255}},
		{"#12345", qtgui.QColor{A: 255}},
		{"blue", qtgui.QColor{A: 255}},
	} {
		if got := parseColour(c.in); got != c.want {
			t.Errorf("parseColour(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestColourName(t *testing.T) {
	// The settings' colours are written back as they were read.
	for _, colour := range []string{"#268bd2", "#000000", "#ffffff", "#66aea79f", "#00ff0000"} {
		if got := colourName(parseColour(colour)); got != colour {
			t.Errorf("colourName(parseColour(%q)) = %q", colour, got)
		}
	}
	// Whatever the case it is read in, it is written in lower case.
	if got := colourName(parseColour("#AbCdEf")); got != "#abcdef" {
		t.Errorf("colourName(parseColour(#AbCdEf)) = %q", got)
	}
}

func TestWithAlpha(t *testing.T) {
	c := qtgui.QColor{R: 10, G: 20, B: 30, A: 255}
	for _, a := range []struct {
		alpha float64
		want  uint8
	}{{0, 0}, {faintOpacity, 102}, {0.5, 128}, {1, 255}, {-1, 0}, {2, 255}} {
		got := withAlpha(c, a.alpha)
		if got.A != a.want || got.R != c.R || got.G != c.G || got.B != c.B {
			t.Errorf("withAlpha(%v) = %+v, want alpha %d", a.alpha, got, a.want)
		}
	}
}

func TestLightness(t *testing.T) {
	// The values are QColor::lightness's, which rounds through 16 bits.
	for _, c := range []struct {
		colour string
		want   int
	}{
		{"#000000", 0},
		{"#ffffff", 255},
		{"#ff0000", 128},
		{"#808080", 128},
		{"#1e1e1e", 30},
		{"#268bd2", 124},
		{"#efefef", 239},
	} {
		if got := lightness(parseColour(c.colour)); got != c.want {
			t.Errorf("lightness(%s) = %d, want %d", c.colour, got, c.want)
		}
	}
}
