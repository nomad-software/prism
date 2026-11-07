package ui

import (
	"cmp"
	"math"
	"slices"
	"time"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/prism/internal/chart"
	"github.com/nomad-software/prism/internal/scene"
	"github.com/nomad-software/prism/internal/stats"
)

// skylineAccent is the colour of the frame and title of the skyline.
var skylineAccent = scene.RGB{R: 0xff, G: 0x2b, B: 0xd6}

// skylineLabels is how many towers are labelled at most: the busiest and
// the largest.
const skylineLabels = 12

// riseTime is how long a tower takes to rise or fall to its new height when
// the processes are read again.
const riseTime = 700 * time.Millisecond

// skyline draws the processes as a neon city at night, seen from above at
// an angle: a tower for each, as tall as it is busy, on as much ground as it
// has memory, with more of its windows lit the busier it is. The towers rise
// and fall smoothly to each reading, their tops pulse and their windows
// flicker, the faster the busier they are.
type skyline struct {
	city scene.Skyline
	// from is the height each tower rises or falls from, from risen, and
	// keys are the processes of the towers, in the order of the towers.
	from  []float64
	risen time.Time
	keys  []stats.ProcessKey
	// labels are the towers named.
	labels []scene.Tower
	// live are the towers drawn every frame once they have risen, those of
	// processes that are busy, which move, and covers, for each, the towers
	// in front of it that overlap it, which are drawn again over it, within
	// it, so that they are still in front. The others are drawn once, into
	// still, under the live ones, with the labels into named, over them.
	live         []bool
	covers       [][]int
	still, named *qt.QPixmap
}

// stirring is how busy a process is at least, in percent of a processor, for
// it to move in a picture: one less busy is drawn once, still.
const stirring = 0.5

// newSkyline returns a screen of the processes as a skyline.
func newSkyline() *screen {
	return newScreen(&skyline{}, "NEON SKYLINE",
		"height = CPU   ·   footprint = memory   ·   lit windows = activity", skylineAccent)
}

func (k *skyline) layout(s *screen, w, h float64, read bool) {
	// Each tower rises or falls from the height it is at now, in cells, or
	// from the ground where it is new; on a resize it is at its height at
	// once.
	before := make(map[stats.ProcessKey]float64, len(k.keys))
	for i, key := range k.keys {
		before[key] = k.height(i, s.now) / max(k.city.Cell, 1)
	}
	area := chart.Rect{X: screenMargin, Y: screenTop, W: max(w-2*screenMargin, 1), H: max(h-screenTop-screenBottom, 1)}
	k.city = scene.LayoutSkyline(s.procs, area)
	k.keys = make([]stats.ProcessKey, len(k.city.Towers))
	k.from = make([]float64, len(k.city.Towers))
	for i, t := range k.city.Towers {
		k.keys[i] = s.procs[t.Index].Key()
		k.from[i] = t.Height
		if read {
			k.from[i] = before[k.keys[i]] * k.city.Cell
		}
	}
	k.risen = s.now
	k.labels = k.chooseLabels(s)
	k.chooseLive(s)
	for _, pixmap := range []*qt.QPixmap{k.still, k.named} {
		if pixmap != nil {
			pixmap.Delete()
		}
	}
	k.still, k.named = nil, nil
}

// chooseLive chooses the towers drawn every frame, those of busy processes,
// and for each the towers in front of it that overlap it, as the towers are
// drawn from the back.
func (k *skyline) chooseLive(s *screen) {
	towers := k.city.Towers
	k.live = make([]bool, len(towers))
	k.covers = make([][]int, len(towers))
	outlines := make([][]chart.XY, len(towers))
	for i, t := range towers {
		outlines[i] = k.city.Outline(t)
	}
	for i, t := range towers {
		if k.live[i] = busy(s.procs[t.Index]) >= stirring; !k.live[i] {
			continue
		}
		for j := i + 1; j < len(towers); j++ {
			if scene.Overlap(outlines[i], outlines[j]) {
				k.covers[i] = append(k.covers[i], j)
			}
		}
	}
}

// rising says whether the towers are still rising or falling at the time.
func (k *skyline) rising(now time.Time) bool {
	return now.Sub(k.risen) < riseTime
}

// height returns how tall the tower of the index is at the time, as it rises
// or falls, slowing as it reaches its height.
func (k *skyline) height(i int, now time.Time) float64 {
	t := k.city.Towers[i]
	f := min(max(float64(now.Sub(k.risen))/float64(riseTime), 0), 1)
	f = 1 - (1-f)*(1-f)*(1-f)
	return k.from[i] + (t.Height-k.from[i])*f
}

func (k *skyline) moving(now time.Time) bool {
	// A tower drawn again over a live one is drawn as it is, still or not.
	return k.rising(now) && len(k.city.Towers) > 0 || slices.Contains(k.live, true)
}

func (k *skyline) at(x, y float64) int {
	if i := k.city.At(x, y); i >= 0 {
		return k.city.Towers[i].Index
	}
	return -1
}

func (k *skyline) outline(i int) *qt.QPainterPath {
	for _, t := range k.city.Towers {
		if t.Index == i {
			return polygonPath(k.city.Outline(t))
		}
	}
	return nil
}

func (k *skyline) drawUnder(p *qt.QPainter, s *screen, w, h float64) {
	fillGradient(p, 0, 0, w, h, scene.RGB{R: 0x07, G: 0x04, B: 0x14}, scene.RGB{R: 0x12, G: 0x06, B: 0x26}, 1, 1)
	// Stars in the sky, the same each time, and a haze of the city's
	// light below them.
	stars := qt.NewQPainterPath()
	for i := range int(w * h / 5000) {
		size := 1.0
		if hash(i, 3) > 0.85 {
			size = 1.6
		}
		stars.AddRect2(hash(i, 1)*w, hash(i, 2)*h*0.4, size, size)
	}
	fillPath(p, stars, white, 0.3)
	stars.Delete()
	fillGradient(p, 0, h*0.15, w, h*0.4, skylineAccent, skylineAccent, 0, 0.1)
	fillGradient(p, 0, h*0.55, w, h*0.3, skylineAccent, skylineAccent, 0.1, 0)

	c := k.city
	if c.N == 0 {
		return
	}
	// The ground, a grid a cell beyond the city on each side, and the light
	// of each tower on it.
	n := float64(c.N)
	ground := qt.NewQPainterPath()
	for i := -1.0; i <= n+1; i++ {
		for _, ends := range [][2]chart.XY{
			{c.Point(i, -1, 0), c.Point(i, n+1, 0)},
			{c.Point(-1, i, 0), c.Point(n+1, i, 0)},
		} {
			ground.MoveTo2(ends[0].X, ends[0].Y)
			ground.LineTo2(ends[1].X, ends[1].Y)
		}
	}
	strokePath(p, ground, skylineAccent, 0.22, 1)
	ground.Delete()
	for _, t := range c.Towers {
		colour := scene.Heat(scene.CPUHeat(s.procs[t.Index]))
		foot := c.Point((t.X0+t.X1)/2, (t.Y0+t.Y1)/2, 0)
		radialGlow(p, foot.X, foot.Y, c.Cell*0.9+t.Height*0.15, colour, 0.25)
	}
}

func (k *skyline) drawOver(*qt.QPainter, *screen, float64, float64) {}

// drawMoving draws the towers as they are at the time. While they rise or
// fall every one is drawn, with the labels; once they have risen, those that
// are still are drawn as they were drawn once, then the live ones, then the
// labels as they were drawn once.
func (k *skyline) drawMoving(p *qt.QPainter, s *screen, now time.Time) {
	seconds := float64(now.UnixMilli()%3_600_000) / 1000
	if k.rising(now) {
		for i, t := range k.city.Towers {
			t.Height = k.height(i, now)
			k.drawTower(p, s, t, seconds)
		}
		k.drawLabels(p, s, now)
		return
	}
	if k.still == nil {
		k.still, k.named = k.paintOnce(s, func(q *qt.QPainter) {
			for i, t := range k.city.Towers {
				if !k.live[i] {
					k.drawTower(q, s, t, 0)
				}
			}
		}), k.paintOnce(s, func(q *qt.QPainter) { k.drawLabels(q, s, now) })
	}
	p.DrawPixmap9(0, 0, k.still)
	towers := k.city.Towers
	for i, t := range towers {
		if !k.live[i] {
			continue
		}
		k.drawTower(p, s, t, seconds)
		if len(k.covers[i]) == 0 {
			continue
		}
		outline := polygonPath(k.city.Outline(t))
		p.SetClipPath(outline)
		for _, j := range k.covers[i] {
			k.drawTower(p, s, towers[j], seconds)
		}
		p.SetClipping(false)
		outline.Delete()
	}
	p.DrawPixmap9(0, 0, k.named)
}

// paintOnce returns a clear pixmap of the size of the screen with what draw
// draws on it.
func (k *skyline) paintOnce(s *screen, draw func(*qt.QPainter)) *qt.QPixmap {
	pixmap := newLookPixmap(look{width: s.look.width, height: s.look.height, ratio: s.look.ratio})
	p := qt.NewQPainter2(pixmap.QPaintDevice)
	p.SetRenderHint(qt.QPainter__Antialiasing)
	draw(p)
	p.End()
	p.Delete()
	return pixmap
}

// drawTower draws the tower, at the seconds into the hour: its two sides,
// darker at the foot, with their windows, and its top, edged in neon that
// pulses.
func (k *skyline) drawTower(p *qt.QPainter, s *screen, t scene.Tower, seconds float64) {
	c := k.city
	proc := s.procs[t.Index]
	heat := scene.CPUHeat(proc)
	colour := scene.Heat(heat)

	left := []chart.XY{c.Point(t.X0, t.Y1, 0), c.Point(t.X1, t.Y1, 0), c.Point(t.X1, t.Y1, t.Height), c.Point(t.X0, t.Y1, t.Height)}
	right := []chart.XY{c.Point(t.X1, t.Y0, 0), c.Point(t.X1, t.Y1, 0), c.Point(t.X1, t.Y1, t.Height), c.Point(t.X1, t.Y0, t.Height)}
	for i, face := range [][]chart.XY{left, right} {
		dark := []float64{0.2, 0.32}[i]
		gradient := qt.NewQLinearGradient3(0, face[0].Y, 0, face[3].Y)
		a, b := qcolour(shade(colour, dark), 1), qcolour(shade(colour, dark*2.2), 1)
		gradient.SetColorAt(0, a)
		gradient.SetColorAt(1, b)
		brush := qt.NewQBrush10(gradient.QGradient)
		path := polygonPath(face)
		p.FillPath(path, brush)
		path.Delete()
		brush.Delete()
		a.Delete()
		b.Delete()
		gradient.Delete()
	}
	// The windows, in rows up each side, three to a row, more of them lit
	// the busier the process is. Each turns on or off now and then, more
	// often the busier it is, at times of its own, but for those of a
	// process that is hardly busy, which stay as they are.
	if c.Cell >= 16 {
		lit := 0.2 + min(busy(proc), 100)/130
		rate := 0.2 + 2.5*heat
		if busy(proc) < stirring {
			rate = 0
		}
		windows := qt.NewQPainterPath()
		for f, face := range [][]chart.XY{left, right} {
			for row := 1; float64(row)*7 < t.Height-3; row++ {
				z := float64(row) * 7
				for col := 1; col <= 3; col++ {
					turn := int(seconds*rate + 10*hash(proc.PID, f, row, col, 1))
					if hash(proc.PID, f, row, col, turn) >= lit {
						continue
					}
					along := float64(col) / 4
					x := face[0].X + (face[1].X-face[0].X)*along
					y := face[0].Y + (face[1].Y-face[0].Y)*along - z
					windows.AddRect2(x-1.5, y-2, 3, 2.5)
				}
			}
		}
		fillPath(p, windows, colour, 0.85)
		windows.Delete()
	}
	// The top pulses, at a time of its own, faster the busier it is. Only
	// a hot tower's edges glow, as the wide strokes of a glow are most of
	// the time a frame takes; the others are edged in a line.
	pulse := 0.75 + 0.25*math.Sin(2*math.Pi*(seconds*(0.3+1.5*heat)+hash(proc.PID)))
	if busy(proc) < stirring {
		pulse = 1
	}
	edge := func(path *qt.QPainterPath, alpha float64) {
		if heat >= hotTower {
			glow(p, path, colour, alpha, 1)
		} else {
			strokePath(p, path, colour, alpha, 1)
		}
	}
	top := polygonPath([]chart.XY{c.Point(t.X0, t.Y0, t.Height), c.Point(t.X1, t.Y0, t.Height), c.Point(t.X1, t.Y1, t.Height), c.Point(t.X0, t.Y1, t.Height)})
	fillPath(p, top, colour, 0.9*pulse)
	edge(top, pulse)
	top.Delete()
	corner := qt.NewQPainterPath()
	front, high := c.Point(t.X1, t.Y1, 0), c.Point(t.X1, t.Y1, t.Height)
	corner.MoveTo2(front.X, front.Y)
	corner.LineTo2(high.X, high.Y)
	edge(corner, 0.8)
	corner.Delete()
}

// hotTower is how hot a tower is at least for its edges to glow.
const hotTower = 0.5

// chooseLabels returns the towers named: the busiest, then the largest.
func (k *skyline) chooseLabels(s *screen) []scene.Tower {
	towers := slices.Clone(k.city.Towers)
	slices.SortStableFunc(towers, func(a, b scene.Tower) int {
		pa, pb := s.procs[a.Index], s.procs[b.Index]
		return cmp.Or(cmp.Compare(busy(pb), busy(pa)), cmp.Compare(pb.Memory, pa.Memory))
	})
	var chosen []scene.Tower
	for _, t := range towers {
		if len(chosen) == skylineLabels {
			break
		}
		if proc := s.procs[t.Index]; busy(proc) >= 1 || proc.MemoryKnown() && proc.Memory > 0 {
			chosen = append(chosen, t)
		}
	}
	return chosen
}

// drawLabels names the towers chosen, as tall as they are at the time, each
// on a line up from the middle of its top and out to the side it is on,
// moved up to clear those named before it.
func (k *skyline) drawLabels(p *qt.QPainter, s *screen, now time.Time) {
	c := k.city
	tops := make([]chart.XY, len(k.labels))
	for i, t := range k.labels {
		height := t.Height
		if j := slices.IndexFunc(c.Towers, func(o scene.Tower) bool { return o.Index == t.Index }); j >= 0 {
			height = k.height(j, now)
		}
		tops[i] = c.Point((t.X0+t.X1)/2, (t.Y0+t.Y1)/2, height)
	}
	order := make([]int, len(k.labels))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return cmp.Compare(tops[a].Y, tops[b].Y) })

	name := s.fixedFont(0.85, true)
	defer name.Delete()
	value := s.fixedFont(0.75, false)
	defer value.Delete()
	var placed []chart.XY
	for _, i := range order {
		top, proc := tops[i], s.procs[k.labels[i].Index]
		colour := scene.Heat(scene.CPUHeat(proc))
		y := top.Y - 30
		for slices.ContainsFunc(placed, func(q chart.XY) bool { return math.Abs(y-q.Y) < 30 && math.Abs(top.X-q.X) < 160 }) {
			y -= 8
		}
		y = max(y, screenTop-20)
		placed = append(placed, chart.XY{X: top.X, Y: y})
		side := 1.0
		if top.X < c.OX {
			side = -1
		}
		end := top.X + side*36
		leader := qt.NewQPainterPath()
		leader.MoveTo2(top.X, top.Y)
		leader.LineTo2(top.X, y+8)
		leader.LineTo2(end, y+8)
		strokePath(p, leader, colour, 0.7, 1)
		leader.Delete()
		radialGlow(p, top.X, top.Y, 5, colour, 0.9)

		const width = 220
		x, flags := end+4, int(qt.AlignLeft|qt.AlignVCenter)
		if side < 0 {
			x, flags = end-4-width, int(qt.AlignRight|qt.AlignVCenter)
		}
		cells := processCells(proc, s.units)
		p.SetFont(name)
		drawShadowText(p, x, y-8, width, 16, flags, proc.Name, white, 1)
		p.SetFont(value)
		drawShadowText(p, x, y+8, width, 14, flags, cells[colProcessCPU]+"  ·  "+cells[colProcessMemory], colour, 0.95)
	}
}
