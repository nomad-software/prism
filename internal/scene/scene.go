// Package scene lays out the pictures the processes can be drawn as: a
// skyline of towers, a tower for each process, and a map of territories of
// hexagons, a territory for each. It needs no Qt.
package scene

import (
	"math"

	"github.com/nomad-software/prism/internal/chart"
	"github.com/nomad-software/prism/internal/usage"
)

// RGB is a colour, each part from 0 to 255.
type RGB struct{ R, G, B uint8 }

// ramp is the colours of heat, from cold to hot: cyan, blue, violet, magenta,
// orange and yellow.
var ramp = []RGB{
	{0x00, 0xe5, 0xff},
	{0x3d, 0x7b, 0xff},
	{0xb1, 0x4b, 0xff},
	{0xff, 0x2b, 0xd6},
	{0xff, 0x6a, 0x3d},
	{0xff, 0xe1, 0x4d},
}

// Heat returns the colour of the heat, from 0, cold, to 1, hot; outside
// that it is as cold or as hot as it goes.
func Heat(t float64) RGB {
	t = min(max(t, 0), 1)
	x := t * float64(len(ramp)-1)
	i := min(int(x), len(ramp)-2)
	f := x - float64(i)
	mix := func(a, b uint8) uint8 { return uint8(math.Round(float64(a) + (float64(b)-float64(a))*f)) }
	a, b := ramp[i], ramp[i+1]
	return RGB{mix(a.R, b.R), mix(a.G, b.G), mix(a.B, b.B)}
}

// CPUHeat returns how hot the process is, from 0 to 1, by how busy it was:
// hottest at a whole processor or more, and counted logarithmically below
// that, so that a little use still shows. One whose use is not known is
// cold.
func CPUHeat(p usage.Process) float64 {
	if !p.Measured {
		return 0
	}
	return min(math.Log1p(max(p.Busy, 0))/math.Log1p(100), 1)
}

// busy returns how busy the process was, or 0 while it is not known.
func busy(p usage.Process) float64 {
	if !p.Measured {
		return 0
	}
	return max(p.Busy, 0)
}

// Inside says whether the point is inside the polygon of the points.
func Inside(polygon []chart.XY, x, y float64) bool {
	in := false
	for i, j := 0, len(polygon)-1; i < len(polygon); j, i = i, i+1 {
		a, b := polygon[i], polygon[j]
		if (a.Y > y) != (b.Y > y) && x < (b.X-a.X)*(y-a.Y)/(b.Y-a.Y)+a.X {
			in = !in
		}
	}
	return in
}

// Overlap says whether two convex polygons overlap, by more than touching.
func Overlap(a, b []chart.XY) bool {
	// They are apart if, across a side of either, one is wholly on one side
	// of a line and the other wholly on the other.
	for _, polygon := range [][]chart.XY{a, b} {
		for i := range polygon {
			p, q := polygon[i], polygon[(i+1)%len(polygon)]
			nx, ny := q.Y-p.Y, p.X-q.X
			project := func(points []chart.XY) (lo, hi float64) {
				lo, hi = math.Inf(1), math.Inf(-1)
				for _, pt := range points {
					d := nx*pt.X + ny*pt.Y
					lo, hi = min(lo, d), max(hi, d)
				}
				return lo, hi
			}
			alo, ahi := project(a)
			blo, bhi := project(b)
			if ahi <= blo || bhi <= alo {
				return false
			}
		}
	}
	return true
}
