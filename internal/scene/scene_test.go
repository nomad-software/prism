package scene

import (
	"math"
	"testing"

	"github.com/nomad-software/prism/internal/chart"
	"github.com/nomad-software/prism/internal/stats"
	"github.com/nomad-software/prism/internal/usage"
)

const mib = 1 << 20

// proc returns a process of the PID, measured at the busy, with the memory.
func proc(pid int, busy float64, memory uint64) usage.Process {
	return usage.Process{Process: stats.Process{PID: pid, Memory: memory}, Busy: busy, Measured: true}
}

func TestHeat(t *testing.T) {
	if got := Heat(0); got != ramp[0] {
		t.Errorf("Heat(0) = %v, want cyan", got)
	}
	if got := Heat(1); got != ramp[len(ramp)-1] {
		t.Errorf("Heat(1) = %v, want yellow", got)
	}
	if Heat(-1) != Heat(0) || Heat(2) != Heat(1) {
		t.Error("heat outside 0 to 1 is not as cold or as hot as it goes")
	}
	if got := Heat(0.6); got != ramp[3] {
		t.Errorf("Heat(0.6) = %v, want magenta", got)
	}
	idle, busy, full := proc(1, 0, 0), proc(2, 10, 0), proc(3, 250, 0)
	if CPUHeat(idle) != 0 || CPUHeat(full) != 1 || CPUHeat(busy) <= 0.4 || CPUHeat(busy) >= 1 {
		t.Errorf("CPUHeat = %v, %v, %v", CPUHeat(idle), CPUHeat(busy), CPUHeat(full))
	}
	if CPUHeat(usage.Process{Busy: 50}) != 0 {
		t.Error("a process not yet measured is not cold")
	}
}

func TestInside(t *testing.T) {
	square := []chart.XY{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}}
	if !Inside(square, 5, 5) || Inside(square, 15, 5) || Inside(square, 5, -1) {
		t.Error("Inside is wrong about a square")
	}
}

func TestOverlap(t *testing.T) {
	square := []chart.XY{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}}
	shift := func(dx, dy float64) []chart.XY {
		var moved []chart.XY
		for _, p := range square {
			moved = append(moved, chart.XY{X: p.X + dx, Y: p.Y + dy})
		}
		return moved
	}
	// A diamond whose box overlaps that of a square, beside its corner.
	diamond := []chart.XY{{X: 25, Y: 20}, {X: 30, Y: 25}, {X: 25, Y: 30}, {X: 20, Y: 25}}
	if !Overlap(square, shift(5, 5)) || Overlap(square, shift(10, 0)) || Overlap(square, shift(20, 0)) {
		t.Error("Overlap is wrong about squares")
	}
	if Overlap(shift(11, 11), diamond) {
		t.Error("a diamond beside a square's corner overlaps it")
	}
	if !Overlap(shift(11, 11), []chart.XY{{X: 19, Y: 19}, {X: 20.5, Y: 19}, {X: 19, Y: 20.5}}) {
		t.Error("a triangle within a square does not overlap it")
	}
}

func TestSkyline(t *testing.T) {
	r := chart.Rect{X: 0, Y: 0, W: 1200, H: 700}
	procs := []usage.Process{proc(10, 0, 4*mib), proc(20, 200, 1024*mib), proc(30, 50, 64*mib), proc(40, 5, 0)}
	s := LayoutSkyline(procs, r)
	if s.N != 3 || len(s.Towers) != len(procs) {
		t.Fatalf("N = %d, %d towers", s.N, len(s.Towers))
	}
	byIndex := map[int]Tower{}
	for _, tower := range s.Towers {
		byIndex[tower.Index] = tower
		for _, pt := range s.Outline(tower) {
			if pt.X < r.X-0.5 || pt.X > r.X+r.W+0.5 || pt.Y < r.Y+LabelRoom-0.5 || pt.Y > r.Y+r.H+0.5 {
				t.Errorf("tower %d reaches %v, out of %+v", tower.Index, pt, r)
			}
		}
	}
	// The largest in memory is in the middle, on the largest ground; the
	// busiest is the tallest, and a process of no memory has a little.
	if big := byIndex[1]; math.Floor(big.X0) != 1 || math.Floor(big.Y0) != 1 {
		t.Errorf("the largest is at %v, %v, not in the middle", big.X0, big.Y0)
	}
	for i, tower := range byIndex {
		if i != 1 && tower.X1-tower.X0 >= byIndex[1].X1-byIndex[1].X0 {
			t.Errorf("tower %d is on as much ground as the largest", i)
		}
		if i != 1 && tower.Height >= byIndex[1].Height {
			t.Errorf("tower %d is as tall as the busiest", i)
		}
		if tower.X1 <= tower.X0 || tower.Height <= 0 {
			t.Errorf("tower %d is empty: %+v", i, tower)
		}
	}
	if byIndex[2].Height <= byIndex[3].Height || byIndex[3].Height <= byIndex[0].Height {
		t.Error("the towers are not as tall as they are busy")
	}
	// Those at the back are first.
	for i := 1; i < len(s.Towers); i++ {
		a, b := s.Towers[i-1], s.Towers[i]
		if math.Floor(a.X0)+math.Floor(a.Y0) > math.Floor(b.X0)+math.Floor(b.Y0) {
			t.Errorf("tower %d is drawn after one in front of it", a.Index)
		}
	}
	// The tower is found at the middle of its top, and none in a corner.
	big := byIndex[1]
	mid := s.Point((big.X0+big.X1)/2, (big.Y0+big.Y1)/2, big.Height)
	if at := s.At(mid.X, mid.Y); at < 0 || s.Towers[at].Index != 1 {
		t.Errorf("At the top of the largest = %d", at)
	}
	if s.At(r.X+1, r.Y+1) != -1 {
		t.Error("a tower is found in the corner")
	}
	if empty := LayoutSkyline(nil, r); len(empty.Towers) != 0 || empty.N != 0 {
		t.Error("there are towers of no processes")
	}
}

// TestSkylineStays checks that a tower keeps its place as its memory and
// use change a little.
func TestSkylineStays(t *testing.T) {
	r := chart.Rect{W: 800, H: 600}
	place := func(procs []usage.Process) map[int][2]float64 {
		places := map[int][2]float64{}
		for _, tower := range LayoutSkyline(procs, r).Towers {
			places[procs[tower.Index].PID] = [2]float64{math.Floor(tower.X0), math.Floor(tower.Y0)}
		}
		return places
	}
	before := place([]usage.Process{proc(1, 5, 100*mib), proc(2, 90, 110*mib), proc(3, 1, 3*mib)})
	after := place([]usage.Process{proc(1, 80, 101*mib), proc(2, 2, 105*mib), proc(3, 40, 3*mib)})
	for pid, at := range before {
		if after[pid] != at {
			t.Errorf("process %d moved from %v to %v", pid, at, after[pid])
		}
	}
}

func TestTerritoryGrid(t *testing.T) {
	tr := grid(10, chart.Rect{X: 20, Y: 30, W: 400, H: 300})
	if tr.Cols == 0 || tr.Rows == 0 {
		t.Fatal("no hexagons")
	}
	cells := tr.Cols * tr.Rows
	for cell := range cells {
		c := tr.Centre(cell)
		for corner := range 6 {
			p := tr.Corner(cell, corner)
			if p.X < 20-0.01 || p.X > 420+0.01 || p.Y < 30-0.01 || p.Y > 330+0.01 {
				t.Fatalf("hexagon %d reaches %v, out of the rectangle", cell, p)
			}
		}
		if tr.At(c.X, c.Y) != cell || tr.At(c.X+3, c.Y-2) != cell {
			t.Fatalf("At the centre of %d = %d", cell, tr.At(c.X, c.Y))
		}
		for side := range 6 {
			n := tr.Neighbour(cell, side)
			if n < 0 {
				continue
			}
			// A neighbour is a hexagon away, across the side, and the
			// hexagon is its neighbour across the side opposite.
			nc := tr.Centre(n)
			if d := math.Hypot(nc.X-c.X, nc.Y-c.Y); math.Abs(d-across(10)) > 0.01 {
				t.Fatalf("neighbour %d of %d is %v away", side, cell, d)
			}
			a, b := tr.Corner(cell, side), tr.Corner(cell, (side+1)%6)
			mid := chart.XY{X: (a.X + b.X) / 2, Y: (a.Y + b.Y) / 2}
			if math.Hypot(mid.X-(c.X+nc.X)/2, mid.Y-(c.Y+nc.Y)/2) > 0.01 {
				t.Fatalf("neighbour %d of %d is not across its side", side, cell)
			}
			if tr.Neighbour(n, (side+3)%6) != cell {
				t.Fatalf("neighbour %d of %d is not its neighbour back", side, cell)
			}
		}
	}
	if tr.At(0, 0) != -1 {
		t.Error("a hexagon is found outside the map")
	}
}

func TestTerritories(t *testing.T) {
	r := chart.Rect{W: 1000, H: 600}
	procs := []usage.Process{proc(1, 50, 2048*mib), proc(2, 0, 512*mib), proc(3, 1, 0), proc(4, 5, 100*mib)}
	tr := LayoutTerritories(procs, r, false)
	if tr.Radius != radii[0] {
		t.Errorf("radius %v, want the largest", tr.Radius)
	}
	total := 0
	for i, p := range procs {
		cells := tr.Cells[i]
		if want := quota(p.Memory, tr.Unit); len(cells) != want {
			t.Errorf("process %d has %d hexagons, want %d", p.PID, len(cells), want)
		}
		for _, c := range cells {
			if tr.Owner[c] != i {
				t.Errorf("hexagon %d of process %d is owned by %d", c, p.PID, tr.Owner[c])
			}
		}
		total += len(cells)
		if !connected(tr, i) {
			t.Errorf("the territory of %d is in pieces", p.PID)
		}
	}
	if total > int(fill*float64(tr.Cols*tr.Rows)) {
		t.Errorf("%d hexagons of %d are owned", total, tr.Cols*tr.Rows)
	}
	// The unit is the smallest that leaves room: half of it would not.
	half := 0
	for _, p := range procs {
		half += quota(p.Memory, tr.Unit/2)
	}
	if half <= int(fill*float64(tr.Cols*tr.Rows)) {
		t.Errorf("unit %d MiB, but %d MiB leaves room", tr.Unit/mib, tr.Unit/2/mib)
	}
	// The largest begins in the middle.
	first := tr.Centre(tr.Cells[0][0])
	if math.Abs(first.X-r.W/2) > across(tr.Radius) || math.Abs(first.Y-r.H/2) > down(tr.Radius)*1.5 {
		t.Errorf("the largest begins at %v, not in the middle", first)
	}

	// Decimal units are 1, 2 or 5 of a power of 10.
	d := LayoutTerritories(procs, r, true)
	for u := d.Unit; u > 9; u /= 10 {
		if u%10 != 0 {
			t.Fatalf("decimal unit %d", d.Unit)
		}
	}

	// Where there is no room for one hexagon each, the smallest have none,
	// and no hexagon has two owners.
	many := make([]usage.Process, 400)
	for i := range many {
		many[i] = proc(i+1, 0, uint64(i)*mib)
	}
	small := LayoutTerritories(many, chart.Rect{W: 120, H: 80}, false)
	owned := 0
	for _, o := range small.Owner {
		if o >= 0 {
			owned++
		}
	}
	if owned != len(small.Owner) {
		t.Errorf("%d of %d hexagons owned, want all", owned, len(small.Owner))
	}
	if len(small.Cells[0]) != 0 || len(small.Cells[399]) == 0 {
		t.Error("the smallest have room before the largest")
	}
	if none := LayoutTerritories(nil, chart.Rect{}, false); len(none.Owner) != 0 {
		t.Error("an empty map has hexagons")
	}
}

// connected says whether the territory of the process is in one piece.
func connected(tr Territories, i int) bool {
	cells := tr.Cells[i]
	if len(cells) == 0 {
		return true
	}
	seen := map[int]bool{cells[0]: true}
	queue := []int{cells[0]}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		for side := range 6 {
			if n := tr.Neighbour(c, side); n >= 0 && tr.Owner[n] == i && !seen[n] {
				seen[n] = true
				queue = append(queue, n)
			}
		}
	}
	return len(seen) == len(cells)
}
