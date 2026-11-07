package scene

import (
	"cmp"
	"math"
	"math/bits"
	"slices"

	"github.com/nomad-software/prism/internal/chart"
	"github.com/nomad-software/prism/internal/usage"
)

// The skyline is seen from above at an angle, its ground a grid of square
// cells drawn as a diamond: a grid unit across is cos30 of a cell across the
// screen and sin30 of one down it.
var (
	cos30 = math.Cos(math.Pi / 6)
	sin30 = 0.5
)

// LabelRoom is the room left above the tallest tower, for its label.
const LabelRoom = 40

// Tower is the tower of a process: the square of ground it stands on, in
// units of the grid, from (X0, Y0) to (X1, Y1), and how tall it is, in
// pixels.
type Tower struct {
	// Index is that of its process, in those laid out.
	Index          int
	X0, Y0, X1, Y1 float64
	Height         float64
}

// Skyline is the towers of the processes, from the back to the front, in the
// order they are drawn, on a grid of N cells by N.
type Skyline struct {
	Towers []Tower
	N      int
	// Cell is how many pixels a cell of the grid is, and (OX, OY) where its
	// back corner is.
	Cell, OX, OY float64
}

// Point returns where the point of the grid, at the height in pixels above
// it, is on the screen.
func (s Skyline) Point(x, y, z float64) chart.XY {
	return chart.XY{X: s.OX + (x-y)*cos30*s.Cell, Y: s.OY + (x+y)*sin30*s.Cell - z}
}

// Outline returns the outline of the tower as it is seen: the back corners
// of its top, its right corner, its front corner and its left corner, at the
// ground, and back up to its top.
func (s Skyline) Outline(t Tower) []chart.XY {
	return []chart.XY{
		s.Point(t.X0, t.Y0, t.Height),
		s.Point(t.X1, t.Y0, t.Height),
		s.Point(t.X1, t.Y0, 0),
		s.Point(t.X1, t.Y1, 0),
		s.Point(t.X0, t.Y1, 0),
		s.Point(t.X0, t.Y1, t.Height),
	}
}

// At returns the index in Towers of the tower at the point, the front one
// where they overlap, or -1 where there is none.
func (s Skyline) At(x, y float64) int {
	for i := len(s.Towers) - 1; i >= 0; i-- {
		if Inside(s.Outline(s.Towers[i]), x, y) {
			return i
		}
	}
	return -1
}

// LayoutSkyline lays the processes out as towers in the rectangle: a tower
// as tall as its process is busy, to a scale of at least a whole processor,
// on a square of ground as large as its memory, the largest square the
// largest memory. The largest in memory stand in the middle, and each keeps
// its place while its memory stays within a power of 2 of what it was.
func LayoutSkyline(procs []usage.Process, r chart.Rect) Skyline {
	if len(procs) == 0 {
		return Skyline{}
	}
	n := max(int(math.Ceil(math.Sqrt(float64(len(procs))))), 3)
	tallest := max(r.H*0.45, 1)
	cell := max(min(r.W/(2*float64(n)*cos30), (r.H-tallest-LabelRoom)/float64(n)), 2)
	tallest = max(min(r.H-float64(n)*cell-LabelRoom, r.H*0.55), 1)
	s := Skyline{N: n, Cell: cell}
	// The towers and the ground are in the middle of the rectangle.
	used := LabelRoom + tallest + float64(n)*cell
	s.OX = r.X + r.W/2
	s.OY = r.Y + max(r.H-used, 0)/2 + LabelRoom + tallest

	top, largest := 100.0, uint64(0)
	for _, p := range procs {
		top = max(top, busy(p))
		largest = max(largest, p.Memory)
	}
	order := make([]int, len(procs))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		return cmp.Or(
			cmp.Compare(bits.Len64(procs[b].Memory), bits.Len64(procs[a].Memory)),
			cmp.Compare(procs[a].PID, procs[b].PID))
	})
	cells := make([][2]int, 0, n*n)
	for x := range n {
		for y := range n {
			cells = append(cells, [2]int{x, y})
		}
	}
	middle := float64(n) / 2
	distance := func(c [2]int) float64 {
		dx, dy := float64(c[0])+0.5-middle, float64(c[1])+0.5-middle
		return dx*dx + dy*dy
	}
	slices.SortStableFunc(cells, func(a, b [2]int) int { return cmp.Compare(distance(a), distance(b)) })

	lowest := min(3, tallest)
	for k, i := range order {
		p, c := procs[i], cells[k]
		foot := 0.3
		if largest > 0 {
			foot += 0.6 * math.Sqrt(float64(p.Memory)/float64(largest))
		}
		margin := (1 - foot) / 2
		x, y := float64(c[0]), float64(c[1])
		s.Towers = append(s.Towers, Tower{
			Index: i,
			X0:    x + margin, Y0: y + margin, X1: x + 1 - margin, Y1: y + 1 - margin,
			Height: lowest + (tallest-lowest)*busy(p)/top,
		})
	}
	// Those at the back are drawn first, for those in front to hide them.
	slices.SortStableFunc(s.Towers, func(a, b Tower) int {
		ax, ay, bx, by := math.Floor(a.X0), math.Floor(a.Y0), math.Floor(b.X0), math.Floor(b.Y0)
		return cmp.Or(cmp.Compare(ax+ay, bx+by), cmp.Compare(ax, bx))
	})
	return s
}
