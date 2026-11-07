package scene

import (
	"cmp"
	"math"
	"math/bits"
	"slices"

	"github.com/nomad-software/prism/internal/chart"
	"github.com/nomad-software/prism/internal/usage"
)

// radii are the sizes the hexagons of a map of territories can be, from
// the centre to a corner in pixels, the largest that leaves room for every
// territory chosen.
var radii = []float64{12, 10, 8, 6, 5, 4}

// fill is how much of the map the territories fill at most, so that they
// are not cramped against its edges.
const fill = 0.7

// Territories is a map of hexagons, Cols across and Rows down, each of Unit
// bytes of memory, in which each process has a territory of as many as its
// memory comes to, and at least one. The hexagons have a corner at the top,
// and each odd row is half a hexagon to the right of the rows either side.
type Territories struct {
	Radius     float64
	Cols, Rows int
	// X0 and Y0 are where the centre of the hexagon at the top left is.
	X0, Y0 float64
	Unit   uint64
	// Owner is the index of the process each hexagon is the territory of,
	// row by row, or -1 for one of none, and Cells the hexagons of each
	// process, the first where it began.
	Owner []int
	Cells [][]int
}

// across returns how far apart the centres of the hexagons are across, and
// down how far apart their rows are.
func across(radius float64) float64 { return math.Sqrt(3) * radius }
func down(radius float64) float64   { return 1.5 * radius }

// Centre returns where the centre of the hexagon is.
func (t Territories) Centre(cell int) chart.XY {
	q, r := cell%t.Cols, cell/t.Cols
	x := t.X0 + float64(q)*across(t.Radius)
	if r%2 == 1 {
		x += across(t.Radius) / 2
	}
	return chart.XY{X: x, Y: t.Y0 + float64(r)*down(t.Radius)}
}

// Corner returns where the corner of the hexagon is, of the six from that
// at the bottom of its right side, clockwise.
func (t Territories) Corner(cell, corner int) chart.XY {
	c := t.Centre(cell)
	a := math.Pi/6 + float64(corner)*math.Pi/3
	return chart.XY{X: c.X + t.Radius*math.Cos(a), Y: c.Y + t.Radius*math.Sin(a)}
}

// neighbours are the steps to the neighbour across each side of a hexagon in
// an even row and in an odd row, by side: the side from each corner to the
// next, below right, below left, left, above left, above right and right.
var neighbours = [2][6][2]int{
	{{0, 1}, {-1, 1}, {-1, 0}, {-1, -1}, {0, -1}, {1, 0}},
	{{1, 1}, {0, 1}, {-1, 0}, {0, -1}, {1, -1}, {1, 0}},
}

// Neighbour returns the hexagon across the side of the hexagon, from its
// corner of the same number to the next, or -1 where that is off the map.
func (t Territories) Neighbour(cell, side int) int {
	q, r := cell%t.Cols, cell/t.Cols
	step := neighbours[r%2][side]
	q, r = q+step[0], r+step[1]
	if q < 0 || q >= t.Cols || r < 0 || r >= t.Rows {
		return -1
	}
	return r*t.Cols + q
}

// At returns the hexagon at the point, or -1 where there is none.
func (t Territories) At(x, y float64) int {
	if t.Cols == 0 || t.Rows == 0 {
		return -1
	}
	row := int(math.Round((y - t.Y0) / down(t.Radius)))
	best, nearest := -1, t.Radius
	for r := row - 1; r <= row+1; r++ {
		if r < 0 || r >= t.Rows {
			continue
		}
		offset := 0.0
		if r%2 == 1 {
			offset = across(t.Radius) / 2
		}
		col := int(math.Round((x - t.X0 - offset) / across(t.Radius)))
		for q := col - 1; q <= col+1; q++ {
			if q < 0 || q >= t.Cols {
				continue
			}
			c := t.Centre(r*t.Cols + q)
			if d := math.Hypot(c.X-x, c.Y-y); d <= nearest {
				best, nearest = r*t.Cols+q, d
			}
		}
	}
	return best
}

// units returns the sizes of memory a hexagon can stand for, from the
// smallest: powers of 2 of mebibytes, or 1, 2 and 5 of each power of 10 of
// megabytes in decimal.
func units(decimal bool) []uint64 {
	var list []uint64
	if decimal {
		for unit := uint64(1e6); unit <= 1e15; unit *= 10 {
			list = append(list, unit, 2*unit, 5*unit)
		}
		return list
	}
	for unit := uint64(1 << 20); unit <= 1<<50; unit *= 2 {
		list = append(list, unit)
	}
	return list
}

// quota returns how many hexagons the memory comes to, of the unit: at least
// one.
func quota(memory, unit uint64) int {
	return max(int((memory+unit/2)/unit), 1)
}

// grid returns the map of hexagons of the radius that fits in the
// rectangle, in its middle, with none of them owned.
func grid(radius float64, r chart.Rect) Territories {
	w, h := across(radius), down(radius)
	t := Territories{Radius: radius}
	t.Cols = max(int((r.W-w/2)/w), 0)
	t.Rows = max(int((r.H-2*radius)/h)+1, 0)
	if r.H < 2*radius {
		t.Rows = 0
	}
	t.X0 = r.X + (r.W-(float64(t.Cols)*w+w/2))/2 + w/2
	t.Y0 = r.Y + (r.H-(float64(max(t.Rows-1, 0))*h+2*radius))/2 + radius
	return t
}

// LayoutTerritories lays the processes out as territories in the rectangle,
// with hexagons as large as leave room for all of them, each of the smallest
// size of memory, in the size units, that does. The largest is in the
// middle, and each other as near the middle as is left, each as round as it
// can be. Where there is no room for all, those with the least memory have
// none.
func LayoutTerritories(procs []usage.Process, r chart.Rect, decimal bool) Territories {
	sizes := units(decimal)
	var t Territories
	found := false
	for _, radius := range radii {
		t = grid(radius, r)
		room := int(fill * float64(t.Cols*t.Rows))
		for _, unit := range sizes {
			total := 0
			for _, p := range procs {
				total += quota(p.Memory, unit)
			}
			if total <= room {
				t.Unit, found = unit, true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Unit = sizes[len(sizes)-1]
	}
	t.grow(procs, r)
	return t
}

// grow gives each process its territory, the largest first, those of as
// many hexagons by the power of 2 their memory is within, so that one keeps
// its place while its memory changes a little.
func (t *Territories) grow(procs []usage.Process, r chart.Rect) {
	cells := t.Cols * t.Rows
	t.Owner = make([]int, cells)
	for i := range t.Owner {
		t.Owner[i] = -1
	}
	t.Cells = make([][]int, len(procs))
	if cells == 0 {
		return
	}
	order := make([]int, len(procs))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		return cmp.Or(
			cmp.Compare(quota(procs[b].Memory, t.Unit), quota(procs[a].Memory, t.Unit)),
			cmp.Compare(bits.Len64(procs[b].Memory), bits.Len64(procs[a].Memory)),
			cmp.Compare(procs[a].PID, procs[b].PID))
	})
	// The hexagons from the middle out, as the map is wide or tall, which
	// each territory begins at the first of that is free.
	middle := chart.XY{X: r.X + r.W/2, Y: r.Y + r.H/2}
	outward := make([]int, cells)
	away := make([]float64, cells)
	for i := range outward {
		outward[i] = i
		c := t.Centre(i)
		dx, dy := (c.X-middle.X)/max(r.W, 1), (c.Y-middle.Y)/max(r.H, 1)
		away[i] = dx*dx + dy*dy
	}
	slices.SortStableFunc(outward, func(a, b int) int { return cmp.Compare(away[a], away[b]) })
	next := 0
	seed := func() int {
		for next < cells && t.Owner[outward[next]] >= 0 {
			next++
		}
		if next == cells {
			return -1
		}
		return outward[next]
	}
	// listed marks the hexagons that are beside the territory being grown,
	// by the number of that territory, from 1.
	listed := make([]int, cells)
	for k, i := range order {
		want := quota(procs[i].Memory, t.Unit)
		var beside []int
		var from chart.XY
		for len(t.Cells[i]) < want {
			// The free hexagon beside the territory nearest where it
			// began, or, where none is, the next from the middle.
			best := -1
			for j, c := range beside {
				if t.Owner[c] >= 0 {
					continue
				}
				if best < 0 || nearer(t.Centre(c), t.Centre(beside[best]), from, c, beside[best]) {
					best = j
				}
			}
			var cell int
			if best >= 0 {
				cell = beside[best]
				beside = slices.Delete(beside, best, best+1)
			} else {
				cell = seed()
				if cell < 0 {
					break
				}
				if len(t.Cells[i]) == 0 {
					from = t.Centre(cell)
				}
			}
			t.Owner[cell] = i
			t.Cells[i] = append(t.Cells[i], cell)
			for side := range 6 {
				if n := t.Neighbour(cell, side); n >= 0 && t.Owner[n] < 0 && listed[n] != k+1 {
					listed[n] = k + 1
					beside = append(beside, n)
				}
			}
		}
	}
}

// nearer says whether a, of the hexagon i, is nearer to the point than b, of
// the hexagon j, or as near and first.
func nearer(a, b, to chart.XY, i, j int) bool {
	da, db := math.Hypot(a.X-to.X, a.Y-to.Y), math.Hypot(b.X-to.X, b.Y-to.Y)
	if da != db {
		return da < db
	}
	return i < j
}
