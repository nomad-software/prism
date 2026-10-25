package settings

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
)

// Colours are the colours graphs are drawn in, each written as Qt names
// colours: #rrggbb, or #aarrggbb with how opaque it is first.
type Colours struct {
	// Grid is the colour of the lines of the grid and the frame of each
	// graph.
	Grid string `json:"grid"`
	// Total is the colour of the line of all of the processors together.
	Total string `json:"total"`
	// Cores are the colours of the lines of each processor, in the order
	// the system numbers them. A processor beyond the end, or whose colour
	// is empty, has its default colour.
	Cores []string `json:"cores,omitempty"`
	// Memory and Swap are the colours of their lines.
	Memory string `json:"memory"`
	Swap   string `json:"swap"`
	// Network are the colours of the lines of each network adapter, by its
	// name. An adapter is given one when it is first seen, which it keeps
	// when it goes away for when it comes back.
	Network map[string]string `json:"network,omitempty"`
}

// Clone returns a copy of the colours that can be changed without changing
// them.
func (c Colours) Clone() Colours {
	c.Cores = slices.Clone(c.Cores)
	c.Network = maps.Clone(c.Network)
	if c.Network == nil {
		c.Network = map[string]string{}
	}
	return c
}

// The default colours are the eight accents of Solarized, which are made to
// be read on a light background and a dark one alike, on a grid of the warm
// grey of Ubuntu's Ambiance, translucent so that it sits back on either.
const (
	blue    = "#268bd2"
	orange  = "#cb4b16"
	cyan    = "#2aa198"
	magenta = "#d33682"
	green   = "#859900"
	violet  = "#6c71c4"
	yellow  = "#b58900"
	red     = "#dc322f"
)

// The default colours of everything but the processors and the adapters.
const (
	DefaultGrid   = "#66aea79f"
	DefaultTotal  = blue
	DefaultMemory = blue
	DefaultSwap   = red
)

// coreColours are the default colours of the first processors, each across
// the wheel of hues from the one before, so that lines next to each other
// are told apart; the rest have variants of them.
var coreColours = []string{green, orange, cyan, magenta, yellow, violet, blue, red}

// adapterColours are the colours network adapters are given first, in
// order, the first two far apart, as most computers have two adapters in
// use; the rest are given variants of them.
var adapterColours = []string{blue, yellow, magenta, cyan, orange, violet, green, red}

// The backgrounds of graphs in the light and the dark theme, and the least
// contrast a variant of a colour has with each, so that its line is seen on
// both.
const (
	lightBackground = "#ffffff"
	darkBackground  = "#1e1e1e"
	minContrast     = 2.2
)

// DefaultColours returns the colours the program starts with.
func DefaultColours() Colours {
	return Colours{
		Grid:    DefaultGrid,
		Total:   DefaultTotal,
		Memory:  DefaultMemory,
		Swap:    DefaultSwap,
		Network: map[string]string{},
	}
}

// DefaultCore returns the default colour of the processor at the index.
func DefaultCore(i int) string {
	return variant(coreColours, i)
}

// Core returns the colour of the processor at the index.
func (c Colours) Core(i int) string {
	if i < len(c.Cores) && Valid(c.Cores[i]) {
		return c.Cores[i]
	}
	return DefaultCore(i)
}

// SetCore sets the colour of the processor at the index.
func (c *Colours) SetCore(i int, colour string) {
	for len(c.Cores) <= i {
		c.Cores = append(c.Cores, "")
	}
	c.Cores[i] = colour
}

// Adapter returns the colour of the network adapter with the name. One that
// has none is given the first of the colours adapters are given that no
// other has, and added says it was, for the settings to be saved.
func (c *Colours) Adapter(name string) (colour string, added bool) {
	if colour, ok := c.Network[name]; ok {
		return colour, false
	}
	if c.Network == nil {
		c.Network = map[string]string{}
	}
	used := make(map[string]bool, len(c.Network))
	for _, colour := range c.Network {
		used[colour] = true
	}
	for i := 0; ; i++ {
		if colour = variant(adapterColours, i); !used[colour] {
			break
		}
	}
	c.Network[name] = colour
	return colour, true
}

// normalise puts a default in place of each colour that is unusable. An
// adapter whose colour is unusable is forgotten, to be given another.
func (c *Colours) normalise() {
	d := DefaultColours()
	for _, field := range []struct {
		value    *string
		fallback string
	}{
		{&c.Grid, d.Grid},
		{&c.Total, d.Total},
		{&c.Memory, d.Memory},
		{&c.Swap, d.Swap},
	} {
		if !Valid(*field.value) {
			*field.value = field.fallback
		}
	}
	for i, colour := range c.Cores {
		if !Valid(colour) {
			c.Cores[i] = ""
		}
	}
	if c.Network == nil {
		c.Network = map[string]string{}
	}
	for name, colour := range c.Network {
		if !Valid(colour) {
			delete(c.Network, name)
		}
	}
}

// Valid says whether a colour is written #rrggbb or #aarrggbb.
func Valid(colour string) bool {
	if len(colour) != 7 && len(colour) != 9 || colour[0] != '#' {
		return false
	}
	_, err := strconv.ParseUint(colour[1:], 16, 32)
	return err == nil
}

// variant returns the colour of the palette for the index: the colours of
// the palette in turn, and beyond its end the same again with their hues
// turned by 10° more each time round, one way then the other, so that no
// two are alike and each is near its own. A variant is made darker, or
// lighter and then paler, as it needs, to be seen on the background of a
// graph in the light theme and in the dark one.
func variant(palette []string, i int) string {
	base := palette[i%len(palette)]
	round := i / len(palette)
	if round == 0 {
		return base
	}
	h, s, v := toHSV(base)
	turn := float64((round+1)/2) * 10
	if round%2 == 1 {
		turn = -turn
	}
	h = math.Mod(h+turn+360, 360)
	colour := base
	for range 100 {
		r, g, b := hsv(h, s, v)
		colour = fmt.Sprintf("#%02x%02x%02x", r, g, b)
		switch {
		case contrast(colour, lightBackground) < minContrast:
			v -= 0.01
		case contrast(colour, darkBackground) < minContrast:
			// A colour already as bright as it goes is made paler.
			if v < 1 {
				v = min(v+0.01, 1)
			} else {
				s -= 0.02
			}
		default:
			return colour
		}
	}
	return colour
}

// toHSV returns the hue in degrees, the saturation and the value, the last
// two from 0 to 1, of a colour written #rrggbb.
func toHSV(colour string) (h, s, v float64) {
	r, g, b := channel(colour, 0), channel(colour, 1), channel(colour, 2)
	high, low := max(r, g, b), min(r, g, b)
	v = high
	if high == 0 {
		return 0, 0, 0
	}
	s = (high - low) / high
	if high == low {
		return 0, s, v
	}
	switch high {
	case r:
		h = 60 * math.Mod((g-b)/(high-low), 6)
	case g:
		h = 60 * ((b-r)/(high-low) + 2)
	default:
		h = 60 * ((r-g)/(high-low) + 4)
	}
	return math.Mod(h+360, 360), s, v
}

// channel returns the red, green or blue, by its index, of a colour written
// #rrggbb, from 0 to 1.
func channel(colour string, i int) float64 {
	n, _ := strconv.ParseUint(colour[1+2*i:3+2*i], 16, 8)
	return float64(n) / 255
}

// luminance returns the relative luminance of a colour written #rrggbb, as
// WCAG reckons it.
func luminance(colour string) float64 {
	var l float64
	for i, weight := range []float64{0.2126, 0.7152, 0.0722} {
		c := channel(colour, i)
		if c <= 0.03928 {
			c /= 12.92
		} else {
			c = math.Pow((c+0.055)/1.055, 2.4)
		}
		l += weight * c
	}
	return l
}

// contrast returns the contrast ratio of two colours written #rrggbb, from
// 1, for the same, to 21, for black and white.
func contrast(a, b string) float64 {
	la, lb := luminance(a), luminance(b)
	return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)
}

// hsv returns the red, green and blue of a hue in degrees, a saturation and
// a value, the last two from 0 to 1.
func hsv(h, s, v float64) (r, g, b uint8) {
	c := v * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := v - c
	var rf, gf, bf float64
	switch {
	case h < 60:
		rf, gf = c, x
	case h < 120:
		rf, gf = x, c
	case h < 180:
		gf, bf = c, x
	case h < 240:
		gf, bf = x, c
	case h < 300:
		rf, bf = x, c
	default:
		rf, bf = c, x
	}
	scale := func(f float64) uint8 { return uint8(math.Round((f + m) * 255)) }
	return scale(rf), scale(gf), scale(bf)
}
