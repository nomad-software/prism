package ui

import (
	"cmp"
	"math"
	"slices"
	"time"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/prism/internal/chart"
	"github.com/nomad-software/prism/internal/scene"
	"github.com/nomad-software/prism/internal/usage"
)

// territoriesAccent is the colour of the frame and title of the
// territories.
var territoriesAccent = scene.RGB{R: 0xff, G: 0xe1, B: 0x4d}

// The panel of the busiest processes, at the right of the territories where
// the screen is at least panelFrom wide, is panelWidth wide and lists
// panelRows of them.
const (
	panelFrom  = 900
	panelWidth = cardWidth
	panelRows  = 10
)

// territories draws the processes as a map of hexagons, each a size of
// memory, on which each process has a territory of as many as its memory
// comes to, glowing as it is busy, in rings that pulse out from where it
// began, the faster the busier it is.
type territories struct {
	land scene.Territories
	// rings are the hexagons of each territory, by how far they are from
	// where it began, in steps of hexagons, over rings, made when the
	// territories are laid out and filled each frame.
	rings [][rings]*qt.QPainterPath
	// panel says whether the panel of the busiest is shown, and pulsing
	// whether any territory pulses.
	panel, pulsing bool
}

// rings is how many hexagons apart the pulses of a territory are.
const rings = 6

// newTerritories returns a screen of the processes as territories.
func newTerritories() *screen {
	return newScreen(&territories{}, "HEX TERRITORIES", "", territoriesAccent)
}

func (t *territories) layout(s *screen, w, h float64, _ bool) {
	t.panel = w >= panelFrom
	right := float64(screenMargin)
	if t.panel {
		right += panelWidth + screenMargin
	}
	area := chart.Rect{X: screenMargin, Y: screenTop, W: max(w-screenMargin-right, 1), H: max(h-screenTop-screenBottom, 1)}
	t.land = scene.LayoutTerritories(s.procs, area, s.units.Decimal)
	s.caption = "each hex = " + s.units.Size(float64(t.land.Unit)) + " of memory   ·   colour + pulse = CPU"

	for _, paths := range t.rings {
		for _, path := range paths {
			if path != nil {
				path.Delete()
			}
		}
	}
	land := t.land
	gap := min(1.5, land.Radius/6)
	t.pulsing = slices.ContainsFunc(land.Cells, func(cells []int) bool { return len(cells) > 0 }) &&
		slices.ContainsFunc(s.procs, func(proc usage.Process) bool { return busy(proc) >= stirring })
	t.rings = make([][rings]*qt.QPainterPath, len(land.Cells))
	for i, cells := range land.Cells {
		if len(cells) == 0 {
			continue
		}
		start := land.Centre(cells[0])
		for _, cell := range cells {
			c := land.Centre(cell)
			ring := int(math.Round(math.Hypot(c.X-start.X, c.Y-start.Y)/(math.Sqrt(3)*land.Radius))) % rings
			if t.rings[i][ring] == nil {
				t.rings[i][ring] = qt.NewQPainterPath()
			}
			t.hexagon(t.rings[i][ring], cell, gap)
		}
	}
}

// live says whether the territory of the process of the index pulses: it
// does when the process is busy.
func (t *territories) live(s *screen, i int) bool {
	return busy(s.procs[i]) >= stirring
}

func (t *territories) moving(time.Time) bool {
	return t.pulsing
}

func (t *territories) at(x, y float64) int {
	if cell := t.land.At(x, y); cell >= 0 {
		return t.land.Owner[cell]
	}
	return -1
}

func (t *territories) outline(i int) *qt.QPainterPath {
	if i < 0 || i >= len(t.land.Cells) || len(t.land.Cells[i]) == 0 {
		return nil
	}
	return t.border(i, 0)
}

// border returns the path of the sides of the territory of the process of
// the index that are not beside more of it, the hexagons drawn smaller by
// the inset.
func (t *territories) border(i int, inset float64) *qt.QPainterPath {
	land := t.land
	path := qt.NewQPainterPath()
	scale := (land.Radius - inset) / land.Radius
	for _, cell := range land.Cells[i] {
		centre := land.Centre(cell)
		for side := range 6 {
			if n := land.Neighbour(cell, side); n >= 0 && land.Owner[n] == i {
				continue
			}
			a, b := land.Corner(cell, side), land.Corner(cell, (side+1)%6)
			path.MoveTo2(centre.X+(a.X-centre.X)*scale, centre.Y+(a.Y-centre.Y)*scale)
			path.LineTo2(centre.X+(b.X-centre.X)*scale, centre.Y+(b.Y-centre.Y)*scale)
		}
	}
	return path
}

// hexagon adds the hexagon, drawn smaller by the gap, to the path.
func (t *territories) hexagon(path *qt.QPainterPath, cell int, gap float64) {
	land := t.land
	centre := land.Centre(cell)
	scale := (land.Radius - gap) / land.Radius
	for corner := range 6 {
		pt := land.Corner(cell, corner)
		x, y := centre.X+(pt.X-centre.X)*scale, centre.Y+(pt.Y-centre.Y)*scale
		if corner == 0 {
			path.MoveTo2(x, y)
		} else {
			path.LineTo2(x, y)
		}
	}
	path.CloseSubpath()
}

func (t *territories) drawUnder(p *qt.QPainter, s *screen, w, h float64) {
	fillGradient(p, 0, 0, w, h, scene.RGB{R: 0x04, G: 0x07, B: 0x0a}, scene.RGB{R: 0x07, G: 0x0b, B: 0x10}, 1, 1)
	land := t.land
	gap := min(1.5, land.Radius/6)
	empty := qt.NewQPainterPath()
	for cell, owner := range land.Owner {
		if owner < 0 {
			t.hexagon(empty, cell, gap)
		}
	}
	fillPath(p, empty, scene.RGB{G: 0xe5, B: 0xff}, 0.035)
	empty.Delete()
	// The territories that do not pulse are filled once, here.
	for i := range t.rings {
		if !t.live(s, i) {
			t.fill(p, s, i, 0)
		}
	}
}

// fill fills the territory of the process of the index as it is at the
// seconds into the hour: its rings of hexagons brighter as a pulse passes out
// through them, faster the busier it is, and not at all for one that is idle.
func (t *territories) fill(p *qt.QPainter, s *screen, i int, seconds float64) {
	heat := scene.CPUHeat(s.procs[i])
	colour := scene.Heat(heat)
	phase := seconds*(0.15+0.9*heat) + hash(s.procs[i].PID)
	for ring, path := range t.rings[i] {
		if path == nil {
			continue
		}
		wave := 0.5 + 0.5*math.Cos(2*math.Pi*(phase-float64(ring)/rings))
		fillPath(p, path, colour, 0.22+0.6*heat*(0.45+0.55*wave))
	}
}

// drawMoving fills each territory that pulses as it is at the time.
func (t *territories) drawMoving(p *qt.QPainter, s *screen, now time.Time) {
	seconds := float64(now.UnixMilli()%3_600_000) / 1000
	for i := range t.rings {
		if t.live(s, i) {
			t.fill(p, s, i, seconds)
		}
	}
}

func (t *territories) drawOver(p *qt.QPainter, s *screen, w, h float64) {
	for i, cells := range t.land.Cells {
		if len(cells) == 0 {
			continue
		}
		border := t.border(i, 0.5)
		glow(p, border, scene.Heat(scene.CPUHeat(s.procs[i])), 0.95, 1.4)
		border.Delete()
	}
	t.drawLabels(p, s)
	if t.panel {
		t.drawPanel(p, s, w, h)
	}
}

// drawLabels names the territories that have room for it, the largest
// first, at their middles, but for those that would be over a name already
// written.
func (t *territories) drawLabels(p *qt.QPainter, s *screen) {
	land := t.land
	order := make([]int, len(land.Cells))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return cmp.Compare(len(land.Cells[b]), len(land.Cells[a])) })
	name := s.fixedFont(0.85, true)
	defer name.Delete()
	value := s.fixedFont(0.75, false)
	defer value.Delete()
	fm := qt.NewQFontMetrics(name)
	defer fm.Delete()
	small := qt.NewQFontMetrics(value)
	defer small.Delete()
	var placed []chart.Rect
	for _, i := range order {
		cells := land.Cells[i]
		if len(cells) < 4 {
			continue
		}
		var sx, sy float64
		left, right := math.Inf(1), math.Inf(-1)
		for _, cell := range cells {
			c := land.Centre(cell)
			sx, sy = sx+c.X, sy+c.Y
			left, right = min(left, c.X), max(right, c.X)
		}
		proc := s.procs[i]
		texts := processCells(proc, s.units)
		values := texts[colProcessCPU] + " · " + texts[colProcessMemory]
		width := float64(max(fm.HorizontalAdvance(proc.Name), small.HorizontalAdvance(values)))
		if width > right-left+4*land.Radius {
			continue
		}
		x, y := sx/float64(len(cells)), sy/float64(len(cells))
		box := chart.Rect{X: x - width/2 - 4, Y: y - 16, W: width + 8, H: 32}
		if slices.ContainsFunc(placed, func(o chart.Rect) bool { return overlaps(o, box) }) {
			continue
		}
		placed = append(placed, box)
		flags := int(qt.AlignHCenter | qt.AlignVCenter)
		p.SetFont(name)
		drawShadowText(p, x-150, y-15, 300, 16, flags, proc.Name, white, 1)
		p.SetFont(value)
		drawShadowText(p, x-150, y+1, 300, 14, flags, values, white, 0.8)
	}
}

// overlaps says whether the rectangles overlap.
func overlaps(a, b chart.Rect) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

// drawPanel lists the busiest processes at the right, below the card, each
// with a bar as long as it is busy, to a scale of at least a processor.
func (t *territories) drawPanel(p *qt.QPainter, s *screen, w, h float64) {
	x := w - screenMargin - panelWidth
	y := float64(screenMargin + cardHeight + screenMargin)
	line := qt.NewQPainterPath()
	line.AddRect2(x-screenMargin/2, y, 1, max(h-y-screenBottom, 0))
	fillPath(p, line, scene.RGB{G: 0xe5, B: 0xff}, 0.25)
	line.Delete()

	heading := s.fixedFont(0.85, true)
	defer heading.Delete()
	p.SetFont(heading)
	drawColourText(p, x, y, panelWidth, 18, int(qt.AlignLeft|qt.AlignVCenter), "// HOTTEST", territoriesAccent, 1)
	list := make([]int, len(s.procs))
	for i := range list {
		list[i] = i
	}
	slices.SortStableFunc(list, func(a, b int) int { return cmp.Compare(busy(s.procs[b]), busy(s.procs[a])) })
	top := 100.0
	if len(list) > 0 {
		top = max(top, busy(s.procs[list[0]]))
	}
	text := s.fixedFont(0.8, false)
	defer text.Delete()
	p.SetFont(text)
	fm := qt.NewQFontMetrics(text)
	defer fm.Delete()
	for row, i := range list[:min(panelRows, len(list))] {
		ry := y + 28 + float64(row)*24
		if ry+20 > h-screenBottom {
			break
		}
		proc := s.procs[i]
		cells := processCells(proc, s.units)
		const nameWidth = 128
		drawColourText(p, x, ry, nameWidth, 16, int(qt.AlignLeft|qt.AlignVCenter), fm.ElidedText(proc.Name, qt.ElideRight, nameWidth-6), white, 0.9)
		bar := qt.NewQPainterPath()
		bar.AddRect2(x+nameWidth, ry+4, max(busy(proc)/top*60, 1), 8)
		fillPath(p, bar, scene.Heat(scene.CPUHeat(proc)), 1)
		bar.Delete()
		drawColourText(p, x, ry, panelWidth, 16, int(qt.AlignRight|qt.AlignVCenter), cells[colProcessCPU], white, 0.7)
	}
}
